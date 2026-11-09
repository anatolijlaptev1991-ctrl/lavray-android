// SPDX-License-Identifier: GPL-3.0-only

package mobile

// platform.go — adapter.PlatformInterface поверх Platform от Kotlin. Портировано
// с experimental/libbox/service.go (sing-box, GPL-3.0-or-later): те же правила
// ответов, но без уведомлений, оболочки, моста, соседей и WIFI, которые на
// телефоне не нужны.
//
// auto_detect_interface при платформе: route.NetworkManager берёт ProtectFunc
// и вызывает AutoDetectInterfaceControl(fd), то есть Platform.Protect, на
// каждый исходящий сокет; к интерфейсу сокет не привязывается. Приложение
// исключено из туннеля (addDisallowedApplication), так что Protect страхует, а
// не определяет маршрут. default_interface имел бы приоритет и привязал бы
// сокеты к имени интерфейса рабочего стола, поэтому patch.go его удаляет.

import (
	"context"
	"net/netip"
	"os"
	"strings"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/process"
	"github.com/sagernet/sing-box/option"
	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/control"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
)

var _ adapter.PlatformInterface = (*boxPlatform)(nil)

// boxPlatform — платформа одного экземпляра sing-box (состояние
// NetworkManager и туннеля у каждого своё), Platform и хаб общие через host.
type boxPlatform struct {
	h *host

	mu          sync.Mutex
	nm          adapter.NetworkManager
	tunName     string
	tunAddrs    []netip.Addr
	defaultIf   *control.Interface
	isExpensive bool
}

func newBoxPlatform(h *host) *boxPlatform { return &boxPlatform{h: h} }

func (p *boxPlatform) networkManager() adapter.NetworkManager {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.nm
}

func (p *boxPlatform) setExpensive(v bool) {
	p.mu.Lock()
	p.isExpensive = v
	p.mu.Unlock()
}

func (p *boxPlatform) Initialize(nm adapter.NetworkManager) error {
	p.mu.Lock()
	p.nm = nm
	p.mu.Unlock()
	return nil
}

func (p *boxPlatform) UsePlatformAutoDetectInterfaceControl() bool { return true }

func (p *boxPlatform) AutoDetectInterfaceControl(fd int) error {
	return p.h.plat.Protect(int32(fd))
}

func (p *boxPlatform) UsePlatformInterface() bool { return true }

func (p *boxPlatform) ProcessPlatformOptions(option.TunPlatformOptions) error { return nil }

func (p *boxPlatform) UsePlatformDefaultInterfaceMonitor() bool { return true }

func (p *boxPlatform) CreateDefaultInterfaceMonitor(l logger.Logger) tun.DefaultInterfaceMonitor {
	return &defaultMonitor{bp: p, logger: l}
}

func (p *boxPlatform) UsePlatformNetworkInterfaces() bool { return true }

func (p *boxPlatform) NetworkInterfaces() ([]adapter.NetworkInterface, error) {
	raw, err := p.h.plat.Interfaces()
	if err != nil {
		return nil, err
	}
	list, err := parseInterfaces(raw)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range list {
		isDefault := list[i].Name != p.tunName && p.defaultIf != nil && list[i].Index == p.defaultIf.Index
		list[i].Expensive = list[i].Expensive || isDefault && p.isExpensive
	}
	return list, nil
}

func (p *boxPlatform) UnderNetworkExtension() bool              { return false }
func (p *boxPlatform) NetworkExtensionIncludeAllNetworks() bool { return false }
func (p *boxPlatform) ClearDNSCache()                           {}
func (p *boxPlatform) RequestPermissionForWIFIState() error     { return nil }
func (p *boxPlatform) UsePlatformWIFIMonitor() bool             { return false }

func (p *boxPlatform) ReadWIFIState(context.Context) adapter.WIFIState { return adapter.WIFIState{} }

// UsePlatformConnectionOwnerFinder всегда true: на Android с платформой
// sing-box в любом случае ищет владельца каждого соединения. Сам Binder-вызов
// делается только если конфиг использует package_name/user (host.ownerLookup);
// иначе возвращается «неизвестный владелец» без обращения к Kotlin.
func (p *boxPlatform) UsePlatformConnectionOwnerFinder() bool { return true }

func (p *boxPlatform) FindConnectionOwner(req *adapter.FindConnectionOwnerRequest) (*adapter.ConnectionOwner, error) {
	if !p.h.ownerLookup.Load() {
		return &adapter.ConnectionOwner{UserId: -1}, nil
	}
	uid, err := p.h.plat.FindOwnerUid(req.IpProtocol, req.SourceAddress, req.SourcePort, req.DestinationAddress, req.DestinationPort)
	if err != nil {
		return nil, err
	}
	if uid < 0 {
		return nil, process.ErrNotFound
	}
	owner := &adapter.ConnectionOwner{UserId: uid}
	for _, pkg := range strings.Split(p.h.plat.PackagesForUid(uid), ",") {
		if pkg = strings.TrimSpace(pkg); pkg != "" {
			owner.AndroidPackageNames = append(owner.AndroidPackageNames, pkg)
		}
	}
	return owner, nil
}

func (p *boxPlatform) UsePlatformNotification() bool                { return false }
func (p *boxPlatform) SendNotification(*adapter.Notification) error { return nil }
func (p *boxPlatform) CancelNotification(string, int32) error       { return nil }

func (p *boxPlatform) MyInterfaceAddress() []netip.Addr {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tunAddrs
}

func (p *boxPlatform) UsePlatformNeighborResolver() bool { return false }
func (p *boxPlatform) StartNeighborMonitor(adapter.NeighborUpdateListener) error {
	return os.ErrInvalid
}
func (p *boxPlatform) CloseNeighborMonitor(adapter.NeighborUpdateListener) error { return nil }

func (p *boxPlatform) UsePlatformShell() bool    { return false }
func (p *boxPlatform) CheckPlatformShell() error { return nil }
func (p *boxPlatform) OpenShellSession(*adapter.PlatformUser, string, []string, string, int32, int32) (adapter.ShellSession, error) {
	return nil, os.ErrInvalid
}
func (p *boxPlatform) LookupUser(string) (*adapter.PlatformUser, error) { return nil, os.ErrInvalid }
func (p *boxPlatform) LookupSFTPServer() (string, error)                { return "", os.ErrInvalid }
func (p *boxPlatform) ReadSystemSSHHostKey() ([]byte, error)            { return nil, os.ErrInvalid }
func (p *boxPlatform) TailscaleHostname() string                        { return "" }

func (p *boxPlatform) UsePlatformBridge() bool { return false }
func (p *boxPlatform) CreateBridge(adapter.BridgeOptions) (adapter.BridgeSession, error) {
	return nil, os.ErrInvalid
}

// OpenInterface открывает TUN через Platform.OpenTun (порт libbox).
func (p *boxPlatform) OpenInterface(options *tun.Options, _ option.TunPlatformOptions) (tun.Tun, error) {
	if len(options.IncludeUID) > 0 || len(options.ExcludeUID) > 0 {
		return nil, E.New("platform: unsupported uid options")
	}
	if len(options.IncludeAndroidUser) > 0 {
		return nil, E.New("platform: unsupported android_user option")
	}
	payload, err := buildTunPayload(options)
	if err != nil {
		return nil, err
	}
	fd, err := p.h.plat.OpenTun(payload)
	if err != nil {
		return nil, err
	}
	options.Name, err = tunnelName(fd)
	if err != nil {
		return nil, E.Cause(err, "query tun name")
	}
	options.InterfaceMonitor.RegisterMyInterface(options.Name)
	dupFD, err := dupFd(fd)
	if err != nil {
		return nil, E.Cause(err, "dup tun file descriptor")
	}
	options.FileDescriptor = dupFD
	p.mu.Lock()
	p.tunName = options.Name
	p.tunAddrs = tunAddresses(options)
	p.mu.Unlock()
	return tun.New(*options)
}

func tunAddresses(o *tun.Options) []netip.Addr {
	out := make([]netip.Addr, 0, len(o.Inet4Address)+len(o.Inet6Address))
	for _, pf := range o.Inet4Address {
		out = append(out, pf.Addr())
	}
	for _, pf := range o.Inet6Address {
		out = append(out, pf.Addr())
	}
	return out
}

func (p *boxPlatform) setDefault(i *control.Interface) {
	p.mu.Lock()
	p.defaultIf = i
	p.mu.Unlock()
}
