// SPDX-License-Identifier: GPL-3.0-only

// update_local.go — VerifyLocalUpdate: офлайн-путь «Установить из файла».
// Установщик уже скопирован оболочкой в staging (путь вне staging — INVALID_ARGUMENT).
// Подпись берётся из файла .minisig рядом (или minisig_path), а без него — из
// манифеста онлайн-проверки. Проверяются подпись роли artifact, продукт,
// платформа и версия: файл не новее установленной версии — valid=false.
package api

import (
	"context"
	"fmt"
	"os"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
)

func (s *Server) VerifyLocalUpdate(ctx context.Context, req *connect.Request[lavrayv1.VerifyLocalUpdateRequest]) (*connect.Response[lavrayv1.VerifyLocalUpdateResponse], error) {
	if req.Msg.InstallerPath == "" {
		return nil, invalidArg("installer_path обязателен")
	}
	installer, err := updates.InsideStaging(s.stagingDir(), req.Msg.InstallerPath)
	if err != nil {
		return nil, invalidArg("installer_path: %v", err)
	}
	sigPath := installer + updates.MinisigSuffix
	if req.Msg.MinisigPath != "" {
		if sigPath, err = updates.InsideStaging(s.stagingDir(), req.Msg.MinisigPath); err != nil {
			return nil, invalidArg("minisig_path: %v", err)
		}
	}
	c, _, err := s.updateClient(lavrayv1.UpdateChannel_UPDATE_CHANNEL_STABLE)
	if err != nil {
		return nil, updateErr(err)
	}
	claims, verr := s.verifyLocalFile(ctx, c, installer, sigPath)
	if verr != nil {
		return connect.NewResponse(&lavrayv1.VerifyLocalUpdateResponse{Errors: []string{verr.Error()}}), nil
	}
	return connect.NewResponse(&lavrayv1.VerifyLocalUpdateResponse{Valid: true, Manifest: &lavrayv1.UpdateManifest{
		Product: claims.Product, Version: claims.Version, Serial: claims.Serial,
		Channel: protoChannel(claims.Channel)}}), nil
}

func protoChannel(ch string) lavrayv1.UpdateChannel {
	if ch == updates.ChannelBeta {
		return lavrayv1.UpdateChannel_UPDATE_CHANNEL_BETA
	}
	return lavrayv1.UpdateChannel_UPDATE_CHANNEL_STABLE
}

// verifyLocalFile проверяет файл целиком и возвращает подписанные заявления.
// Файл подписи читается с пределом 4 КиБ; его нет — подпись берётся из манифеста.
func (s *Server) verifyLocalFile(ctx context.Context, c *updates.Client, installer, sigPath string) (updates.ArtifactClaims, error) {
	sha, digest, size, err := updates.HashInstaller(installer)
	if err != nil {
		return updates.ArtifactClaims{}, fmt.Errorf("installer_path: %w", err)
	}
	text, err := updates.ReadSignatureFile(sigPath)
	fromManifest := os.IsNotExist(err)
	if fromManifest {
		text, err = s.signatureFromManifest(ctx, c, sha, size)
	}
	if err != nil {
		return updates.ArtifactClaims{}, err
	}
	pol := updates.LocalPolicy{Product: c.Product, Platform: platformOf(c), AppVersion: s.upd.appVersion()}
	claims, err := updates.VerifyInstallerSignature(c.Trust, text, digest, pol)
	if err == nil && (fromManifest || sigPath != installer+updates.MinisigSuffix) {
		// Оболочка перед установкой сама проверит файл по .minisig рядом с ним: подпись из
		// манифеста сохраняется, иначе принятый ядром файл она отвергла бы (signature_invalid).
		err = writeSignatureSidecar(installer, text)
	}
	return claims, err
}

// writeSignatureSidecar кладёт «<установщик>.minisig» атомарно (временный файл и переименование).
func writeSignatureSidecar(installer string, text []byte) error {
	final := installer + updates.MinisigSuffix
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, text, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func platformOf(c *updates.Client) string {
	if c.Platform != "" {
		return c.Platform
	}
	return updates.CurrentAssetKey()
}

// signatureFromManifest: подписи рядом нет — ищем актив с тем же sha256 в
// манифесте онлайн-проверки (нужна сеть и действующая цепочка).
func (s *Server) signatureFromManifest(ctx context.Context, c *updates.Client, sha string, size int64) ([]byte, error) {
	res, err := c.Check(ctx)
	if err != nil || res.Manifest == nil || res.Manifest.App == nil {
		return nil, fmt.Errorf("рядом с установщиком нет файла .minisig, а манифест недоступен")
	}
	for _, a := range res.Manifest.App.Assets {
		if a.Sha256 == sha && a.Length == size {
			return []byte(a.Minisig), nil
		}
	}
	return nil, fmt.Errorf("рядом с установщиком нет файла .minisig, а в манифесте нет такого файла")
}
