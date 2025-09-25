package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Masterminds/log-go"
)

func assemble(msg mail.Message) []byte {
	buf := new(bytes.Buffer)
	for h := range msg.Header {
		if strings.HasPrefix(h, "Yamn-") {
			log.Errorf("Ignoring internal mail header in assemble phase: %s", h)
		} else {
			buf.WriteString(h + ": " + msg.Header.Get(h) + "\n")
		}
	}
	buf.WriteString("\n")
	buf.ReadFrom(msg.Body)
	return buf.Bytes()
}

// headToAddy parses a header containing email addresses
func headToAddy(h mail.Header, header string) (addys []string) {
	_, exists := h[header]
	if !exists {
		return
	}
	addyList, err := h.AddressList(header)
	if err != nil {
		log.Warnf("Failed to parse header: %s", header)
	}
	for _, addy := range addyList {
		addys = append(addys, addy.Address)
	}
	return
}

type emailAddress struct {
	name   string
	domain string
}

// splitAddress splits an email address into its component parts
func splitEmailAddress(addy string) (e emailAddress, err error) {
	if !strings.Contains(addy, "@") {
		err = fmt.Errorf("%s: Email address contains no '@'", addy)
		return
	}
	components := strings.Split(addy, "@")
	if len(components) != 2 {
		err = fmt.Errorf("%s: Malformed email address", addy)
		return
	}
	e.name = components[0]
	e.domain = components[1]
	return
}

// mxLookup returns the responsible MX for a given email address
func mxLookup(email string) (relay string, err error) {
	emailParts, err := splitEmailAddress(email)
	if err != nil {
		return
	}
	
	// Special handling for .onion domains - don't attempt DNS lookup
	if strings.HasSuffix(emailParts.domain, ".onion") {
		relay = emailParts.domain
		log.Tracef("Detected .onion address: %s - Using direct routing", emailParts.domain)
		return relay, nil
	}
	
	// DNS MX lookup per domini normali
	mxRecords, err := net.LookupMX(emailParts.domain)
	if err != nil {
		relay = emailParts.domain
		log.Tracef("DNS MX lookup failed for %s. Using hostname.", emailParts.domain)
		return relay, nil
	}
	
	for _, mx := range mxRecords {
		if !cfg.Mail.OnionRelay {
			// Skip .onion MX se non abilitato
			if strings.HasSuffix(mx.Host, ".onion.") {
				continue
			}
		}
		relay = mx.Host
		break
	}
	
	if relay == "" {
		log.Infof("No valid MX records found for %s. Using hostname.", emailParts.domain)
		relay = emailParts.domain
	}
	
	log.Tracef("DNS lookup: Hostname=%s, MX=%s", emailParts.domain, relay)
	return relay, nil
}

// parseFrom takes a mail address of the format Name <name@foo> and validates it
func parseFrom(h mail.Header) []string {
	from, err := h.AddressList("From")
	if err != nil {
		return []string{fmt.Sprintf("%s <%s>", cfg.Mail.OutboundName, cfg.Mail.OutboundAddy)}
	}
	if len(from) == 0 {
		return []string{fmt.Sprintf("%s <%s>", cfg.Mail.OutboundName, cfg.Mail.OutboundAddy)}
	}
	if cfg.Mail.CustomFrom {
		return []string{fmt.Sprintf("%s <%s>", from[0].Name, from[0].Address)}
	}
	if len(from[0].Name) == 0 {
		return []string{fmt.Sprintf("%s <%s>", cfg.Mail.OutboundName, cfg.Mail.OutboundAddy)}
	}
	return []string{fmt.Sprintf("%s <%s>", from[0].Name, cfg.Mail.OutboundAddy)}
}

// mailPoolFile reads a file from the outbound pool and mails it
func mailPoolFile(filename string) (delFlag bool, err error) {
	delFlag = false

	f, err := os.Open(filename)
	if err != nil {
		log.Errorf("Failed to read file for mailing: %s", err)
		return
	}
	defer f.Close()

	msg, err := mail.ReadMessage(f)
	if err != nil {
		log.Errorf("Failed to process mail file: %s", err)
		delFlag = true
		return
	}

	// Test for a Pooled Date header
	pooledHeader := msg.Header.Get("Yamn-Pooled-Date")
	if pooledHeader == "" {
		log.Warn("No Yamn-Pooled-Date header in message")
	} else {
		var pooledDate time.Time
		pooledDate, err = time.Parse(shortdate, pooledHeader)
		if err != nil {
			log.Errorf("%s: Failed to parse Yamn-Pooled-Date: %s", filename, err)
			return
		}
		age := daysAgo(pooledDate)
		if age > cfg.Pool.MaxAge {
			log.Infof("%s: Refusing to mail pool file. Exceeds max age of %d days", filename, cfg.Pool.MaxAge)
			delFlag = true
			return
		}
		if age > 0 {
			log.Tracef("Mailing pooled file that's %d days old.", age)
		}
		delete(msg.Header, "Yamn-Pooled-Date")
	}

	// Add required headers
	msg.Header["Date"] = []string{time.Now().Format(rfc5322date)}
	msg.Header["Message-Id"] = []string{messageID()}
	msg.Header["From"] = parseFrom(msg.Header)
	sendTo := headToAddy(msg.Header, "To")
	sendTo = append(sendTo, headToAddy(msg.Header, "Cc")...)
	if len(sendTo) == 0 {
		err = fmt.Errorf("%s: No email recipients found", filename)
		delFlag = true
		return
	}

	err = mailBytes(assemble(*msg), sendTo)
	return
}

// mailBytes sends a byte payload to given addresses
func mailBytes(payload []byte, sendTo []string) (err error) {
	log.Tracef("Message recipients are: %s", strings.Join(sendTo, ","))
	
	if cfg.Mail.Outfile {
		var f *os.File
		filename := randPoolFilename("outfile-")
		log.Tracef("Writing output to %s", filename)
		f, err = os.Create(filename)
		if err != nil {
			log.Warnf("Pool file creation failed: %s\n", err)
			return
		}
		defer f.Close()
		_, err = f.WriteString(string(payload))
		if err != nil {
			log.Warnf("Outfile write failed: %s\n", err)
			return
		}
		return
	}
	
	if cfg.Mail.Pipe != "" {
		return execSend(payload, cfg.Mail.Pipe)
	}
	
	if cfg.Mail.Sendmail {
		return sendmailTor(payload, sendTo)
	}
	
	return smtpRelay(payload, sendTo)
}

// execSend pipes mail to an external command
func execSend(payload []byte, execCmd string) (err error) {
	sendmail := new(exec.Cmd)
	sendmail.Args = strings.Fields(execCmd)
	sendmail.Path = sendmail.Args[0]

	stdin, err := sendmail.StdinPipe()
	if err != nil {
		log.Errorf("%s: %s", execCmd, err)
		return
	}
	defer stdin.Close()
	sendmail.Stdout = os.Stdout
	sendmail.Stderr = os.Stderr
	err = sendmail.Start()
	if err != nil {
		log.Errorf("%s: %s", execCmd, err)
		return
	}
	stdin.Write(payload)
	stdin.Close()
	err = sendmail.Wait()
	if err != nil {
		log.Errorf("%s: %s", execCmd, err)
		return
	}
	return
}

// shouldUseTor determina se usare Tor per un indirizzo
func shouldUseTor(address string) bool {
	if cfg.Tor == nil || !cfg.Tor.Enabled {
		return false
	}
	
	// Sempre Tor per indirizzi .onion
	if strings.Contains(address, ".onion") {
		return true
	}
	
	// Forza Tor se configurato
	if cfg.Mail.ForceTorSMTP {
		return true
	}
	
	return false
}

// smtpRelay sends email via SMTP
func smtpRelay(payload []byte, sendTo []string) (err error) {
	// Determina se usare Tor basandosi sui destinatari
	useTor := false
	for _, recipient := range sendTo {
		if shouldUseTor(recipient) {
			useTor = true
			break
		}
	}
	
	conf := &tls.Config{InsecureSkipVerify: true}
	relay := cfg.Mail.SMTPRelay
	port := cfg.Mail.SMTPPort
	
	// Handle MX lookup for direct delivery (skip for .onion)
	if cfg.Mail.MXRelay && len(sendTo) == 1 && !strings.Contains(sendTo[0], ".onion") && !useTor {
		log.Tracef("DNS lookup of MX record for %s", sendTo[0])
		mx, err := mxLookup(sendTo[0])
		if err == nil {
			log.Tracef("Doing direct relay for %s to %s:25", sendTo[0], mx)
			relay = mx
			port = 25
		}
	}
	
	serverAddr := fmt.Sprintf("%s:%d", relay, port)
	
	var conn net.Conn
	timeout := time.Duration(cfg.Tor.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	
	// Create connection
	if useTor {
		conn, err = dialThroughTor(serverAddr)
		if err != nil {
			log.Warnf("Tor SMTP dial error for %s: %v", serverAddr, err)
			return
		}
	} else {
		conn, err = net.DialTimeout("tcp", serverAddr, timeout)
		if err != nil {
			log.Warnf("Direct SMTP dial error for %s: %v", serverAddr, err)
			return
		}
	}

	client, err := smtp.NewClient(conn, relay)
	if err != nil {
		log.Warnf("SMTP connection error for %s: %v", serverAddr, err)
		return
	}
	defer client.Quit()
	
	// STARTTLS (skip for .onion if configured)
	ok, _ := client.Extension("STARTTLS")
	shouldUseTLS := cfg.Mail.UseTLS
	if strings.Contains(relay, ".onion") && cfg.Mail.DisableTLSOnion {
		shouldUseTLS = false
	}
	
	if ok && shouldUseTLS {
		if err = client.StartTLS(conf); err != nil {
			log.Warnf("STARTTLS error for %s: %v", serverAddr, err)
			return
		}
	}
	
	// Authentication
	ok, _ = client.Extension("AUTH")
	if ok && cfg.Mail.Username != "" && cfg.Mail.Password != "" {
		auth := smtp.PlainAuth("", cfg.Mail.Username, cfg.Mail.Password, cfg.Mail.SMTPRelay)
		if err = client.Auth(auth); err != nil {
			log.Warnf("SMTP auth error for %s: %v", serverAddr, err)
			return
		}
	}
	
	// Sender
	sender := cfg.Mail.Sender
	if sender == "" {
		sender = cfg.Remailer.Address
	}
	
	if err = client.Mail(sender); err != nil {
		log.Warnf("SMTP MAIL error for %s: %v", serverAddr, err)
		return
	}
	
	// Recipients
	for _, addr := range sendTo {
		if err = client.Rcpt(addr); err != nil {
			log.Warnf("SMTP RCPT error for %s: %v", addr, err)
			return
		}
	}
	
	// Data
	w, err := client.Data()
	if err != nil {
		log.Warnf("SMTP DATA error: %v", err)
		return
	}
	
	_, err = w.Write(payload)
	if err != nil {
		log.Warnf("SMTP write error: %v", err)
		return
	}
	
	err = w.Close()
	if err != nil {
		log.Warnf("SMTP close error: %v", err)
		return
	}
	
	if useTor {
		log.Tracef("Email sent via Tor to %s", serverAddr)
	} else {
		log.Tracef("Email sent directly to %s", serverAddr)
	}
	return nil
}

// sendmail invokes standard sendmail
func sendmail(payload []byte, sendTo []string) (err error) {
	auth := smtp.PlainAuth("", cfg.Mail.Username, cfg.Mail.Password, cfg.Mail.SMTPRelay)
	relay := fmt.Sprintf("%s:%d", cfg.Mail.SMTPRelay, cfg.Mail.SMTPPort)
	err = smtp.SendMail(relay, auth, cfg.Remailer.Address, sendTo, payload)
	if err != nil {
		log.Warn(err)
		return
	}
	return
}

// sendmailTor routes sendmail through Tor when enabled
func sendmailTor(payload []byte, sendTo []string) (err error) {
	if cfg.Tor == nil || !cfg.Tor.Enabled {
		return sendmail(payload, sendTo)
	}
	
	// When using Tor, use SMTP relay instead of direct sendmail
	log.Info("Tor enabled - routing through SMTP relay instead of direct sendmail")
	return smtpRelay(payload, sendTo)
}
