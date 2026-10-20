package smtptest_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/smtp"
	"github.com/a-novel-kit/golib/smtp/smtptest"
)

func TestSender(t *testing.T) {
	t.Parallel()

	sender := smtptest.NewSender()

	require.NoError(t, sender.SendMail(smtp.MailUsers{{Email: "user"}}, nil, "", map[string]string{"test": "foo"}))
	require.NoError(t, sender.SendMail(smtp.MailUsers{{Email: "user"}}, nil, "", map[string]string{"test": "bar"}))

	testCases := []struct {
		name string

		value string

		expect *smtptest.Mail
	}{
		{
			name:   "Success/First",
			value:  "foo",
			expect: &smtptest.Mail{To: []string{"user"}, Data: map[string]string{"test": "foo"}},
		},
		{
			name:   "Success/Second",
			value:  "bar",
			expect: &smtptest.Mail{To: []string{"user"}, Data: map[string]string{"test": "bar"}},
		},
		{
			name:  "Error/NotSent",
			value: "baz",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mail, ok := sender.FindMail(func(mail *smtptest.Mail) bool {
				return mail.Data.(map[string]string)["test"] == testCase.value
			})

			require.Equal(t, testCase.expect != nil, ok)
			require.Equal(t, testCase.expect, mail)
		})
	}
}

func TestPing(t *testing.T) {
	t.Parallel()

	sender := smtptest.NewSender()
	require.ErrorIs(t, sender.Ping(), smtptest.ErrPing)
}
