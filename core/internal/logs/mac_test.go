// SPDX-License-Identifier: GPL-3.0-only

package logs

import "testing"

func TestMaskMACs(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{
			"строка sing-box о шлюзе",
			"network: updated network environment: gateway 192.168.31.1, gateway_mac 50:88:11:22:33:44",
			"network: updated network environment: gateway 192.168.31.1, gateway_mac 50:88:**:**:**:**",
		},
		{"верхний регистр", "mac AA:BB:CC:DD:EE:FF up", "mac AA:BB:**:**:**:** up"},
		{"разделитель «-»", "adapter 50-88-11-22-33-44 up", "adapter 50-88-**-**-**-** up"},
		{"два адреса в строке", "src 50:88:11:22:33:44 dst a0:b1:c2:d3:e4:f5.", "src 50:88:**:**:**:** dst a0:b1:**:**:**:**."},
		{"в скобках и после равно", "[50:88:11:22:33:44] mac=a0:b1:c2:d3:e4:f5,", "[50:88:**:**:**:**] mac=a0:b1:**:**:**:**,"},
		{"в начале и в конце строки", "50:88:11:22:33:44", "50:88:**:**:**:**"},
		{"уже замаскированный не меняется", "gateway_mac 50:88:**:**:**:**", "gateway_mac 50:88:**:**:**:**"},
	}
	for _, c := range cases {
		got := MaskMACs(c.in)
		if got != c.want {
			t.Errorf("%s:\n  вход  %q\n  итог  %q\n  ждали %q", c.name, c.in, got, c.want)
		}
		if again := MaskMACs(got); again != got {
			t.Errorf("%s: повторное маскирование меняет строку: %q -> %q", c.name, got, again)
		}
	}
}

// Чужие сообщения не ломаются: на всём, что не MAC, строка возвращается как есть.
func TestMaskMACsLeavesOtherMessagesAlone(t *testing.T) {
	for _, in := range []string{
		"",
		"INFO[0000] inbound/mixed[mixed-in]: tcp server started at 127.0.0.1:2080",
		"outbound/vless[proxy]: connection to example.com:443",
		"2026-10-29 12:34:56 +0300 INFO router: found rule-set 12:34:56",
		"ipv6 2001:db8:85a3:0:0:8a2e:370:7334 reachable",          // 8 групп, не MAC
		"ipv6 fe80::aabb:ccdd:eeff:1122",                          // сжатый IPv6
		"ipv6 2001:db8:aa:bb:cc:dd:ee:ff",                         // шесть двухзначных групп внутри IPv6
		"eui-64 50:88:11:22:33:44:55:66",                          // 8 байтов, не MAC
		"uuid 3b0f2c8e-6a4d-4c1e-9b7a-2f5d8e1a0c34",               // UUID
		"time 12:34:56:78:90 short",                               // 5 групп
		"смешанные разделители 50:88-11:22-33:44",                 // не MAC
		"hex 1aa:bb:cc:dd:ee:ff и aa:bb:cc:dd:ee:ff1",             // трёхзначные границы
		"port 8080 addr 192.168.31.1 version 1.14.2 id 1234-5678", // обычный текст
		"ключ: значение; пробелы - и тире",
	} {
		if got := MaskMACs(in); got != in {
			t.Errorf("строка изменена:\n  вход  %q\n  итог  %q", in, got)
		}
	}
}

// Журнал ядра (и экспорт из UI, который читает кольцо) получает уже
// замаскированную строку; секреты по-прежнему редактируются.
func TestMuxLogfMasksMACs(t *testing.T) {
	m := NewMux(10)
	m.Logf(LevelInfo, SourceSingBox, "network: updated network environment: gateway %s, gateway_mac %s",
		"192.168.31.1", "50:88:11:22:33:44")
	m.Logf(LevelWarn, SourceCore, "token=abc123secret mac a0:b1:c2:d3:e4:f5")
	m.Logf(LevelInfo, SourceCore, "обычная строка, tcp 127.0.0.1:2080")

	got := m.Tail(0)
	want := []string{
		"network: updated network environment: gateway 192.168.31.1, gateway_mac 50:88:**:**:**:**",
		"token=[REDACTED] mac a0:b1:**:**:**:**",
		"обычная строка, tcp 127.0.0.1:2080",
	}
	if len(got) != len(want) {
		t.Fatalf("строк %d, ожидалось %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Message != w {
			t.Errorf("строка %d:\n  итог  %q\n  ждали %q", i, got[i].Message, w)
		}
	}
}
