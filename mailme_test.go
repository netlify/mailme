package mailme

import (
	"bufio"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// smtpMessage is a message received by the server from startSMTPServer.
type smtpMessage struct {
	from string   // envelope sender, from MAIL FROM
	to   []string // envelope recipients, from RCPT TO
	data string   // raw DATA
}

// startSMTPServer runs a minimal SMTP server that accepts one message and sends it to
// the returned channel.
func startSMTPServer(t *testing.T) (string, int, <-chan smtpMessage) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	messages := make(chan smtpMessage, 1)
	go func() {
		defer ln.Close()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		reply := func(s string) { conn.Write([]byte(s + "\r\n")) }
		address := func(line, command string) string {
			return strings.Trim(strings.TrimSpace(line)[len(command):], "<>")
		}

		var msg smtpMessage
		reply("220 localhost ESMTP")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				reply("250 localhost")
			case strings.HasPrefix(cmd, "MAIL FROM:"):
				msg.from = address(line, "MAIL FROM:")
				reply("250 ok")
			case strings.HasPrefix(cmd, "RCPT TO:"):
				msg.to = append(msg.to, address(line, "RCPT TO:"))
				reply("250 ok")
			case strings.HasPrefix(cmd, "DATA"):
				reply("354 go ahead")
				var data strings.Builder
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if l == ".\r\n" {
						break
					}
					data.WriteString(l)
				}
				msg.data = data.String()
				messages <- msg
				reply("250 ok")
			case strings.HasPrefix(cmd, "QUIT"):
				reply("221 bye")
				return
			default:
				reply("250 ok")
			}
		}
	}()

	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	portNum, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return host, portNum, messages
}

// receive waits for the message sent to the server from startSMTPServer.
func receive(t *testing.T, messages <-chan smtpMessage) smtpMessage {
	select {
	case msg := <-messages:
		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the message")
		return smtpMessage{}
	}
}

func TestMailHeaders(t *testing.T) {
	host, port, messages := startSMTPServer(t)
	m := &Mailer{
		Host:   host,
		Port:   port,
		From:   "sender@example.com",
		Logger: logrus.New(),
		Headers: map[string]string{
			"X-Custom-Id": "abc-123",
			"x-lower-id":  "def-456",
			"X-Empty":     "",
			// Names without the X- prefix are skipped. These are headers Mail sets itself,
			// in any case.
			"From":    "spoofed@example.com",
			"Subject": "Spoofed subject",
			"from":    "lower-spoofed@example.com",
			"TO":      "attacker@example.com",
			"subject": "Lower spoofed subject",
			// Sender, Cc and Bcc would change who the mail is sent from and to.
			"Sender": "envelope-spoofed@example.com",
			"Cc":     "cc@example.com",
			"bcc":    "lower-bcc@example.com",
			"Bcc":    "bcc@example.com",
			// gomail writes these itself, so they would be sent twice.
			"Content-Type":              "text/plain",
			"Content-Transfer-Encoding": "base64",
			"MIME-Version":              "2.0",
			"date":                      "Mon, 01 Jan 2001 00:00:00 +0000",
			// Any other standard header.
			"Reply-To": "reply@example.com",
			"":         "Empty name",
			// Invalid names with the X- prefix could add header lines of their own.
			"X-Injected\r\nFrom": "injected@example.com",
			"X-Has Space":        "Space in name",
			"X-Colon:":           "Colon in name",
		},
	}

	err := m.Mail("user@example.com", "Hello {{ .Name }}", "", "<p>Hi {{ .Name }}</p>", map[string]interface{}{"Name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}

	msg := receive(t, messages)
	if msg.from != "sender@example.com" {
		t.Errorf("envelope sender = %q, want %q", msg.from, "sender@example.com")
	}
	if len(msg.to) != 1 || msg.to[0] != "user@example.com" {
		t.Errorf("envelope recipients = %q, want [user@example.com]", msg.to)
	}
	for _, want := range []string{
		"X-Custom-Id: abc-123\r\n",
		"x-lower-id: def-456\r\n",
		"From: sender@example.com\r\n",
		"To: user@example.com\r\n",
		"Subject: Hello Ada\r\n",
		"<p>Hi Ada</p>",
	} {
		if !strings.Contains(msg.data, want) {
			t.Errorf("message missing %q:\n%s", want, msg.data)
		}
	}
	for _, unwanted := range []string{
		"X-Empty", "spoofed@example.com", "Spoofed subject", "attacker@example.com", "Lower spoofed subject",
		"envelope-spoofed@example.com", "cc@example.com", "bcc@example.com",
		"Content-Type: text/plain", "Content-Transfer-Encoding: base64", "MIME-Version", "Mon, 01 Jan 2001",
		"reply@example.com", "Empty name",
		"X-Injected", "injected@example.com", "Space in name", "Colon in name",
	} {
		if strings.Contains(msg.data, unwanted) {
			t.Errorf("message unexpectedly contains %q:\n%s", unwanted, msg.data)
		}
	}
}

func TestMailWithoutHeaders(t *testing.T) {
	host, port, messages := startSMTPServer(t)
	m := &Mailer{Host: host, Port: port, From: "sender@example.com", Logger: logrus.New()}

	if err := m.Mail("user@example.com", "Hello", "", "<p>Hi</p>", nil); err != nil {
		t.Fatal(err)
	}

	msg := receive(t, messages)
	if !strings.Contains(msg.data, "Subject: Hello\r\n") || !strings.Contains(msg.data, "<p>Hi</p>") {
		t.Errorf("unexpected message:\n%s", msg.data)
	}
}

func TestIsCustomHeader(t *testing.T) {
	for name, want := range map[string]bool{
		"X-Custom-Id":  true,
		"x-custom-id":  true,
		"X-":           false, // prefix only
		"X":            false,
		"":             false,
		"XY-Custom-Id": false,
		"Content-Type": false,
		"Bcc":          false,
	} {
		if got := isCustomHeader(name); got != want {
			t.Errorf("isCustomHeader(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestIsValidHeaderName(t *testing.T) {
	for name, want := range map[string]bool{
		"X-Custom-Id":        true,
		"X-Bang!":            true, // lowest allowed character
		"X-Tilde~":           true, // highest allowed character
		"":                   false,
		"X Space":            false, // just below '!'
		"X-Del\x7f":          false, // just above '~'
		"X-Colon:":           false,
		"X-Injected\r\nFrom": false,
		"X-Ünicode":          false,
	} {
		if got := isValidHeaderName(name); got != want {
			t.Errorf("isValidHeaderName(%q) = %v, want %v", name, got, want)
		}
	}
}
