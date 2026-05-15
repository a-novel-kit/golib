package smtptest_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/smtp"
	"github.com/a-novel-kit/golib/smtp/smtptest"
)

func TestSender(t *testing.T) {
	t.Parallel()

	sender := smtptest.NewSender()

	require.NoError(t, sender.SendMail(smtp.MailUsers{{Email: "user"}}, nil, "", map[string]string{"test": "foo"}))
	require.NoError(t, sender.SendMail(smtp.MailUsers{{Email: "user"}}, nil, "", map[string]string{"test": "bar"}))

	require.Eventually(t, func() bool {
		res, ok := sender.FindMail(func(mail *smtptest.Mail) bool {
			return mail.Data.(map[string]string)["test"] == "foo"
		})

		return assert.True(t, ok) &&
			assert.Equal(t, &smtptest.Mail{
				To:   []string{"user"},
				Data: map[string]string{"test": "foo"},
			}, res)
	}, 100*time.Millisecond, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		res, ok := sender.FindMail(func(mail *smtptest.Mail) bool {
			return mail.Data.(map[string]string)["test"] == "bar"
		})

		return assert.True(t, ok) &&
			assert.Equal(t, &smtptest.Mail{
				To:   []string{"user"},
				Data: map[string]string{"test": "bar"},
			}, res)
	}, 100*time.Millisecond, 10*time.Millisecond)

	require.Eventually(t, func() bool {
		res, ok := sender.FindMail(func(mail *smtptest.Mail) bool {
			return mail.Data.(map[string]string)["test"] == "baz"
		})

		return assert.False(t, ok) &&
			assert.Nil(t, res)
	}, 100*time.Millisecond, 10*time.Millisecond)
}

func TestPing(t *testing.T) {
	t.Parallel()

	sender := smtptest.NewSender()
	require.ErrorIs(t, sender.Ping(), smtptest.ErrPing)
}
