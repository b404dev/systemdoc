package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"systemdoc/internal/remote"
)

type sshPreference struct {
	KeySetupOffered bool   `json:"key_setup_offered"`
	Identity        string `json:"identity,omitempty"`
}

func sshTarget(o remote.Options) string {
	host, user := o.Host, o.User
	if embedded, name, ok := strings.Cut(host, "@"); ok {
		host, user = name, embedded
	}
	return fmt.Sprintf("%s@%s:%d", user, host, o.Port)
}

func (w *workspace) remoteDialog() {
	host := tview.NewInputField().SetLabel("SSH host / alias").SetFieldWidth(44)
	username := tview.NewInputField().SetLabel("SSH username").SetFieldWidth(32).SetPlaceholder("Blank uses SSH config")
	binary := tview.NewInputField().SetLabel("Remote executable").SetText("systemdoc").SetFieldWidth(44)
	identity := tview.NewInputField().SetLabel("SSH private key (optional)").SetFieldWidth(44).SetPlaceholder("Saved key or SSH config")
	upload := tview.NewCheckbox().SetLabel("Upload temporary matching binary")
	userScope := tview.NewCheckbox().SetLabel("Inspect the login user's services")
	form := tview.NewForm().AddFormItem(host).AddFormItem(username).AddFormItem(binary).AddFormItem(identity).AddFormItem(upload).AddFormItem(userScope)
	form.SetBorder(true).SetTitle(" CONNECT TO LINUX · SSH ")
	status := textView().SetWrap(true)
	status.SetText(" First connection offers SSH key setup. Host checks, upload and the app share\n one authenticated connection; passwords and passphrases stay with OpenSSH.")
	close := func() { w.pages.RemovePage("remote"); w.app.SetFocus(w.table) }
	connect := func(forceSetup bool) {
		options := remote.Options{Host: strings.TrimSpace(host.GetText()), User: strings.TrimSpace(username.GetText()), Binary: strings.TrimSpace(binary.GetText()), Upload: upload.IsChecked(), Identity: strings.TrimSpace(identity.GetText())}
		if userScope.IsChecked() {
			options.Args = []string{"--user"}
		}
		if err := options.Validate(); err != nil {
			status.SetText(" " + err.Error()).SetTextColor(tcell.GetColor(w.palette().error))
			return
		}
		if strings.HasPrefix(options.Identity, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				status.SetText(err.Error())
				return
			}
			options.Identity = filepath.Join(home, strings.TrimPrefix(options.Identity, "~/"))
		}
		preference := w.settings.SSHConnections[sshTarget(options)]
		if options.Identity == "" {
			options.Identity = preference.Identity
		}
		if forceSetup || !preference.KeySetupOffered {
			w.offerSSHKey(options, form)
		} else {
			close()
			w.connectRemote(options, false)
		}
	}
	form.AddButton("Connect", func() { connect(false) }).AddButton("Set up key", func() { connect(true) }).AddButton("Cancel", close)
	form.SetCancelFunc(close)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(form, 0, 1, true).AddItem(status, 3, 0, false)
	w.pages.AddPage("remote", centered(panel, 90, 24), true, true)
}

func (w *workspace) offerSSHKey(options remote.Options, form *tview.Form) {
	key := options.Identity
	if key == "" {
		var err error
		key, err = remote.DefaultKey()
		if err != nil {
			w.message("SSH key setup", err.Error())
			return
		}
	}
	modal := tview.NewModal().SetText(tview.Escape("Set up SSH key access for " + options.Host + "?\nLogin: " + available(options.User) + " (blank uses SSH configuration)\n\nKey: " + key + "\n\nSet up key creates this key if needed and copies only its public key to the remote account using ssh-copy-id. OpenSSH prompts for authentication. Your private key stays here.\n\nUse existing SSH keeps your current keys, agent or password login.")).AddButtons([]string{"Set up key", "Use existing SSH", "Cancel"})
	modal.SetDoneFunc(func(index int, _ string) {
		w.pages.RemovePage("ssh-key-offer")
		if index < 0 || index == 2 {
			w.app.SetFocus(form)
			return
		}
		if index == 0 {
			options.Identity = key
		}
		w.pages.RemovePage("remote")
		w.app.SetFocus(w.table)
		w.connectRemote(options, index == 0)
	})
	w.pages.AddPage("ssh-key-offer", modal, true, true)
}

func (w *workspace) connectRemote(options remote.Options, setup bool) {
	if setup {
		var err error
		w.app.Suspend(func() { err = remote.InstallKey(options, options.Identity) })
		if err != nil {
			w.message("SSH key setup failed", err.Error())
			return
		}
	}
	if w.settings.SSHConnections == nil {
		w.settings.SSHConnections = map[string]sshPreference{}
	}
	w.settings.SSHConnections[sshTarget(options)] = sshPreference{KeySetupOffered: true, Identity: options.Identity}
	w.savePreferences()
	var err error
	w.app.Suspend(func() { err = remote.Run(options) })
	if err != nil {
		w.message("Remote session", err.Error())
	}
}
