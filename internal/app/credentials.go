package app

import (
	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/i18n"
)

// credentialsDialog changes the token of the environment the session
// talks to. The token proves this client may ask it for work, so it is
// kept and tested before it is saved.
func credentialsDialog(c *ui.Context, s *Session) {
	modal(c, &s.showCredentials, func() {
		t := c.Theme()
		dialogPanel(c, 460).Children(func() {
			text(c, i18n.T("Environment token"), 16, t.Text, 600)
			faint(c, i18n.T("Test the new token before saving it. Your drafts and queued messages are kept."), 11.5)
			ui.Field(c, i18n.T("Token"), func() {
				ui.TextInput(c, &s.credentialToken).
					Placeholder(i18n.T("Paste the environment token")).Password()
			})
			if s.credentialError != "" {
				errorNote(c, s.credentialError)
			}
			if s.connected {
				faint(c, i18n.T("The environment keeps the token it is given; the old one works until the new one does."), 11)
			}
			dialogFooter(c, func() {
				if ui.Button(c, i18n.T("Cancel")).Clicked() {
					s.closeCredentials()
				}
				save := ui.PrimaryButton(c, i18n.T("Save and reconnect"))
				if s.connecting {
					save.Disabled(true)
				}
				save.OnClick(s.updateCredentials)
			})
		})
	})
}

// environmentAliasDialog names an environment of this client. The alias
// is what the lists show, so a machine can carry the name its owner
// gave it.
func environmentAliasDialog(c *ui.Context, s *Session) {
	modal(c, &s.aliasOpen, func() {
		t := c.Theme()
		dialogPanel(c, 420).Children(func() {
			text(c, i18n.T("Environment alias"), 16, t.Text, 600)
			faint(c, i18n.T("Saved on this client. Leave blank to use the hostname."), 11.5)
			ui.Field(c, i18n.T("Alias"), func() {
				ui.TextInput(c, &s.aliasValue).Placeholder(i18n.T("Alias (blank uses hostname)"))
			})
			dialogFooter(c, func() {
				if ui.Button(c, i18n.T("Cancel")).Clicked() {
					s.closeEnvironmentAlias()
				}
				ui.PrimaryButton(c, i18n.T("Save alias")).OnClick(func() {
					s.saveEnvironmentAlias()
				})
			})
		})
	})
}
