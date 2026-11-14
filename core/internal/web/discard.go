// SPDX-License-Identifier: GPL-3.0-only

package web

import (
	"io"
	"net/http"
)

// maxDiscardBody — сколько тела отклонённого запроса дочитывается; столько же
// net/http сам дочитывает за обработчиком, чтобы переиспользовать соединение.
const maxDiscardBody = 256 << 10

// DiscardBody дочитывает тело запроса, на который отвечают отказом. Если
// клиент прислал Connection: close, сервер после ответа закрывает соединение,
// не дочитав тело, и непрочитанные байты в приёмном буфере превращают
// закрытие в RST: клиент вместо 401 видит обрыв (прогон 0.9.0, D-4: 3,5 %
// запросов urllib на Windows).
func DiscardBody(r *http.Request) {
	if r.Body != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, maxDiscardBody))
	}
}

// rejectRPC — отказ шлюза с дочитанным телом запроса.
func rejectRPC(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	DiscardBody(r)
	writeRPCError(w, status, code, msg)
}
