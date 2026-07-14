package usecase

import (
	"encoding/csv"
	"fmt"
	"log"
	"net/smtp"
	"os"

	"github.com/xvlet/vjm/internal/domain"
)

// SendMailsIfNeeded parses the JTL file to count successes and failures,
// then triggers SMTP emails for any MailerResultCollector that exceeded its thresholds.
func SendMailsIfNeeded(jtlPath string, collectors []*domain.ResultCollector) error {
	var mailers []*domain.MailerModel
	for _, rc := range collectors {
		if rc.MailerModel != nil {
			mailers = append(mailers, rc.MailerModel)
		}
	}

	if len(mailers) == 0 {
		return nil
	}

	file, err := os.Open(jtlPath)
	if err != nil {
		return fmt.Errorf("failed to open JTL file for mailer: %w", err)
	}
	defer func() { _ = file.Close() }()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return fmt.Errorf("failed to read JTL CSV: %w", err)
	}

	if len(records) <= 1 {
		// No data or only header
		return nil
	}

	header := records[0]
	successIdx := -1
	for i, col := range header {
		if col == "success" {
			successIdx = i
			break
		}
	}

	if successIdx == -1 {
		return fmt.Errorf("could not find 'success' column in JTL")
	}

	successCount := 0
	failureCount := 0

	for _, row := range records[1:] {
		if len(row) > successIdx {
			if row[successIdx] == "true" {
				successCount++
			} else {
				failureCount++
			}
		}
	}

	log.Printf("[Mailer] JTL Parsed - Successes: %d, Failures: %d", successCount, failureCount)

	for _, m := range mailers {
		if m.FailureLimit > 0 && failureCount >= m.FailureLimit {
			log.Printf("[Mailer] Failure limit reached (%d >= %d), sending failure email...", failureCount, m.FailureLimit)
			if err := sendEmail(m, m.FailureSubject, fmt.Sprintf("Failure limit exceeded. Failures: %d", failureCount)); err != nil {
				log.Printf("[Mailer] Error sending failure email: %v", err)
			}
		}
		if m.SuccessLimit > 0 && successCount >= m.SuccessLimit {
			log.Printf("[Mailer] Success limit reached (%d >= %d), sending success email...", successCount, m.SuccessLimit)
			if err := sendEmail(m, m.SuccessSubject, fmt.Sprintf("Success limit exceeded. Successes: %d", successCount)); err != nil {
				log.Printf("[Mailer] Error sending success email: %v", err)
			}
		}
	}

	return nil
}

func sendEmail(m *domain.MailerModel, subject, body string) error {
	if m.SmtpHost == "" || m.ToAddress == "" || m.FromAddress == "" {
		return fmt.Errorf("incomplete SMTP configuration")
	}

	port := m.SmtpPort
	if port == "" {
		port = "25"
	}

	addr := fmt.Sprintf("%s:%s", m.SmtpHost, port)

	var auth smtp.Auth
	if m.Username != "" {
		auth = smtp.PlainAuth("", m.Username, m.Password, m.SmtpHost)
	}

	msg := []byte(fmt.Sprintf("To: %s\r\n"+
		"From: %s\r\n"+
		"Subject: %s\r\n"+
		"\r\n"+
		"%s\r\n", m.ToAddress, m.FromAddress, subject, body))

	err := smtp.SendMail(addr, auth, m.FromAddress, []string{m.ToAddress}, msg)
	return err
}
