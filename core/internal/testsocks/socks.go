// SPDX-License-Identifier: GPL-3.0-only

// Package testsocks — локальный SOCKS5-сервер для тестов (T-84): CONNECT и
// UDP ASSOCIATE без авторизации, запоминает адреса назначения. Служит
// «сервером профиля» в регрессиях URL- и полного теста: тест видит, что трафик
// пришёл именно через исходящее профиля и к какой цели. Только на loopback.
package testsocks

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

// Server — слушающий SOCKS5-сервер.
type Server struct {
	ln net.Listener

	mu       sync.Mutex
	tcpDests []string
	udpDests []string
	conns    map[net.Conn]struct{}
	closed   bool
	wg       sync.WaitGroup
}

// Start поднимает сервер на 127.0.0.1:<свободный порт>; закрывается по t.Cleanup.
func Start(t testing.TB) *Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("testsocks: %v", err)
	}
	s := &Server{ln: ln, conns: map[net.Conn]struct{}{}}
	s.wg.Add(1)
	go s.acceptLoop()
	t.Cleanup(s.Close)
	return s
}

// Port — порт сервера.
func (s *Server) Port() int { return s.ln.Addr().(*net.TCPAddr).Port }

// TCPDestinations — цели CONNECT в порядке прихода ("host:port" как в запросе).
func (s *Server) TCPDestinations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.tcpDests...)
}

// UDPDestinations — цели UDP-датаграмм через ASSOCIATE.
func (s *Server) UDPDestinations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.udpDests...)
}

// Close останавливает сервер и рвёт все соединения.
func (s *Server) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	_ = s.ln.Close()
	for c := range s.conns {
		_ = c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Server) track(c net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.conns[c] = struct{}{}
	return true
}

func (s *Server) untrack(c net.Conn) {
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
	_ = c.Close()
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		if !s.track(c) {
			_ = c.Close()
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer s.untrack(c)
			_ = s.serve(c)
		}()
	}
}

func (s *Server) serve(c net.Conn) error {
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	head := make([]byte, 2)
	if _, err := io.ReadFull(c, head); err != nil || head[0] != 5 {
		return errors.New("testsocks: не SOCKS5")
	}
	if _, err := io.CopyN(io.Discard, c, int64(head[1])); err != nil {
		return err
	}
	if _, err := c.Write([]byte{5, 0}); err != nil {
		return err
	}
	req := make([]byte, 3)
	if _, err := io.ReadFull(c, req); err != nil {
		return err
	}
	host, port, err := readAddr(c)
	if err != nil {
		return err
	}
	dest := net.JoinHostPort(host, strconv.Itoa(port))
	_ = c.SetDeadline(time.Time{})
	switch req[1] {
	case 1:
		return s.connect(c, dest)
	case 3:
		return s.associate(c)
	}
	_, _ = c.Write([]byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
	return fmt.Errorf("testsocks: команда %d не поддерживается", req[1])
}

func (s *Server) connect(c net.Conn, dest string) error {
	s.mu.Lock()
	s.tcpDests = append(s.tcpDests, dest)
	s.mu.Unlock()
	up, err := net.DialTimeout("tcp", dest, 5*time.Second)
	if err != nil {
		_, _ = c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return err
	}
	defer up.Close()
	if _, err := c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return err
	}
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(up, c); _ = up.Close(); done <- struct{}{} }()
	go func() { _, _ = io.Copy(c, up); _ = c.Close(); done <- struct{}{} }()
	<-done
	return nil
}

// associate обслуживает UDP ASSOCIATE: relay-сокет на loopback, датаграммы
// клиента пересылаются по назначению, ответы возвращаются с заголовком SOCKS5.
// Ассоциация живёт, пока открыто управляющее TCP-соединение.
func (s *Server) associate(c net.Conn) error {
	relay, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return err
	}
	defer relay.Close()
	out, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return err
	}
	defer out.Close()
	reply := []byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0}
	binary.BigEndian.PutUint16(reply[8:], uint16(relay.LocalAddr().(*net.UDPAddr).Port))
	if _, err := c.Write(reply); err != nil {
		return err
	}

	var clientMu sync.Mutex
	var client *net.UDPAddr
	go func() { // ответы целей → клиенту
		buf := make([]byte, 64<<10)
		for {
			n, from, err := out.ReadFromUDP(buf)
			if err != nil {
				return
			}
			clientMu.Lock()
			to := client
			clientMu.Unlock()
			if to == nil {
				continue
			}
			pkt := append([]byte{0, 0, 0, 1}, from.IP.To4()...)
			pkt = binary.BigEndian.AppendUint16(pkt, uint16(from.Port))
			pkt = append(pkt, buf[:n]...)
			_, _ = relay.WriteToUDP(pkt, to)
		}
	}()
	go func() { // датаграммы клиента → цели
		buf := make([]byte, 64<<10)
		for {
			n, from, err := relay.ReadFromUDP(buf)
			if err != nil {
				return
			}
			clientMu.Lock()
			client = from
			clientMu.Unlock()
			host, port, rest, err := parseDatagram(buf[:n])
			if err != nil {
				continue
			}
			dest := net.JoinHostPort(host, strconv.Itoa(port))
			s.mu.Lock()
			s.udpDests = append(s.udpDests, dest)
			s.mu.Unlock()
			to, err := net.ResolveUDPAddr("udp", dest)
			if err != nil {
				continue
			}
			_, _ = out.WriteToUDP(rest, to)
		}
	}()
	_, _ = io.Copy(io.Discard, c) // управляющее соединение закрыто → конец ассоциации
	return nil
}

// readAddr читает ATYP + адрес + порт запроса.
func readAddr(r io.Reader) (host string, port int, err error) {
	atyp := make([]byte, 1)
	if _, err = io.ReadFull(r, atyp); err != nil {
		return
	}
	switch atyp[0] {
	case 1:
		b := make([]byte, 4)
		if _, err = io.ReadFull(r, b); err == nil {
			host = net.IP(b).String()
		}
	case 4:
		b := make([]byte, 16)
		if _, err = io.ReadFull(r, b); err == nil {
			host = net.IP(b).String()
		}
	case 3:
		l := make([]byte, 1)
		if _, err = io.ReadFull(r, l); err != nil {
			return
		}
		b := make([]byte, l[0])
		if _, err = io.ReadFull(r, b); err == nil {
			host = string(b)
		}
	default:
		err = fmt.Errorf("testsocks: ATYP %d", atyp[0])
	}
	if err != nil {
		return
	}
	p := make([]byte, 2)
	if _, err = io.ReadFull(r, p); err == nil {
		port = int(binary.BigEndian.Uint16(p))
	}
	return
}

// parseDatagram разбирает UDP-датаграмму SOCKS5: RSV(2) FRAG ATYP ADDR PORT DATA.
func parseDatagram(b []byte) (host string, port int, data []byte, err error) {
	if len(b) < 4 || b[2] != 0 {
		return "", 0, nil, errors.New("testsocks: плохая датаграмма")
	}
	var n int
	switch b[3] {
	case 1:
		if len(b) < 10 {
			return "", 0, nil, errors.New("testsocks: короткая датаграмма")
		}
		host, n = net.IP(b[4:8]).String(), 8
	case 4:
		if len(b) < 22 {
			return "", 0, nil, errors.New("testsocks: короткая датаграмма")
		}
		host, n = net.IP(b[4:20]).String(), 20
	case 3:
		if len(b) < 5 || len(b) < 7+int(b[4]) {
			return "", 0, nil, errors.New("testsocks: короткая датаграмма")
		}
		host, n = string(b[5:5+int(b[4])]), 5+int(b[4])
	default:
		return "", 0, nil, fmt.Errorf("testsocks: ATYP %d", b[3])
	}
	return host, int(binary.BigEndian.Uint16(b[n : n+2])), b[n+2:], nil
}
