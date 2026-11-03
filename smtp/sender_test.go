package smtp_test

import (
	"net/mail"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/smtp"
)

func TestMailUser(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		user smtp.MailUser

		expect string
	}{
		{
			name: "Success",

			user: smtp.MailUser{Name: "Jane Doe", Email: "jane@example.com"},

			expect: `"Jane Doe" <jane@example.com>`,
		},
		{
			name: "Success/NoName",

			user: smtp.MailUser{Email: "jane@example.com"},

			expect: "<jane@example.com>",
		},
		{
			// A comma would otherwise split the address list in two.
			name: "Success/Specials",

			user: smtp.MailUser{Name: `Doe, "Jane"`, Email: "jane@example.com"},

			expect: `"Doe, \"Jane\"" <jane@example.com>`,
		},
		{
			name: "Success/NonASCII",

			user: smtp.MailUser{Name: "Équipe Agora", Email: "team@example.com"},

			expect: "=?utf-8?q?=C3=89quipe_Agora?= <team@example.com>",
		},
		{
			// A line break in a name never reaches the header raw, so it cannot add a field.
			name: "Success/LineBreak",

			user: smtp.MailUser{Name: "Jane\r\nBcc: eve@example.com", Email: "jane@example.com"},

			expect: "=?utf-8?b?SmFuZQ0KQmNjOiBldmVAZXhhbXBsZS5jb20=?= <jane@example.com>",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := testCase.user.String()
			require.Equal(t, testCase.expect, got)

			// RFC 5322 §3.4 allows only printable ASCII in a header without SMTPUTF8, and the
			// mailbox reads back to the same participant.
			requireHeaderASCII(t, got)

			parsed, err := mail.ParseAddress(got)
			require.NoError(t, err)
			require.Equal(t, testCase.user.Name, parsed.Name)
			require.Equal(t, testCase.user.Email, parsed.Address)
		})
	}
}

func TestMailUsers(t *testing.T) {
	t.Parallel()

	users := smtp.MailUsers{{Name: "Jane", Email: "jane@example.com"}, {Email: "john@example.com"}}

	require.Equal(t, `"Jane" <jane@example.com>, <john@example.com>`, users.String())

	parsed, err := mail.ParseAddressList(users.String())
	require.NoError(t, err)
	require.Len(t, parsed, len(users))

	for i, user := range users {
		require.Equal(t, user.Name, parsed[i].Name)
		require.Equal(t, user.Email, parsed[i].Address)
	}

	require.Equal(t, []string{"jane@example.com", "john@example.com"}, users.Emails())
	require.Empty(t, smtp.MailUsers{}.String())
}

// requireHeaderASCII fails unless header holds printable ASCII and spaces only, as a header field
// body without SMTPUTF8 does.
func requireHeaderASCII(t *testing.T, header string) {
	t.Helper()

	for i := range len(header) {
		if c := header[i]; c < ' ' || c > '~' {
			require.Failf(t, "non-ASCII header", "byte %#x at offset %d in %q", c, i, header)
		}
	}
}
