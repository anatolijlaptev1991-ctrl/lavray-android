// SPDX-License-Identifier: GPL-3.0-only

// client_download.go — скачивание установщика проверенного результата Check:
// подпись minisign проверяется ключами роли artifact корня доверия, trusted
// comment обязан совпасть с «product;platform;channel;version;serial» манифеста.
package updates

import (
	"context"
	"fmt"
)

// Staged — проверенный установщик в каталоге staging.
type Staged struct {
	Path    string
	Sha256  string
	Version string
	Channel string
	Serial  int64
}

// ArtifactVerifierFor собирает проверку подписи актива res: разбор minisig из
// манифеста, ключи роли artifact, ожидаемые заявления.
func (r *Result) ArtifactVerifierFor(asset Asset) (ArtifactVerifier, error) {
	if r.Manifest == nil || r.Manifest.App == nil {
		return nil, fmt.Errorf("%w: в манифесте нет приложения", ErrNoCheck)
	}
	sig, err := ParseMinisig([]byte(asset.Minisig))
	if err != nil {
		return nil, err
	}
	mf := r.Manifest
	claims := ArtifactClaims{Product: mf.Product, Platform: r.AssetKey, Channel: mf.Channel,
		Version: mf.App.Version, Serial: mf.Serial}
	root := r.Trust.Root()
	return func(d [DigestSize]byte) error { return root.VerifyArtifactSignature(sig, d, claims) }, nil
}

// Download скачивает актив платформы доступного обновления в dir (staging).
func (c *Client) Download(ctx context.Context, res *Result, dir string, progress func(done, total int64)) (*Staged, error) {
	if res == nil || !res.Availability.Available {
		return nil, fmt.Errorf("%w: обновления нет", ErrNoCheck)
	}
	asset, ok := res.Manifest.App.Assets[res.AssetKey]
	if !ok {
		return nil, fmt.Errorf("%w: нет актива %q", ErrNoCheck, res.AssetKey)
	}
	verify, err := res.ArtifactVerifierFor(asset)
	if err != nil {
		return nil, err
	}
	dl := &Downloader{Sources: res.Mirrors, Dir: dir, Verify: verify, Progress: progress, Signature: asset.Minisig}
	p, err := dl.Fetch(ctx, asset)
	if err != nil {
		return nil, err
	}
	_, _ = CleanStaging(dir, p, 0, c.now()) // чужие установщики и обрывки не копятся
	return &Staged{Path: p, Sha256: asset.Sha256, Version: res.Manifest.App.Version,
		Channel: res.Manifest.Channel, Serial: res.Manifest.Serial}, nil
}
