package dashboard

import (
	"context"
	"os/exec"
	"os/user"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Elevated probes need sudo, and the reader should not leave the workspace to
// type a password. sudo -n -v tells us whether credentials are cached; when
// they are not, a masked field asks once and hands the password to sudo -S on
// stdin. The password is never logged, stored or echoed.

func sudoCached(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "sudo", "-n", "-v").Run() == nil
}

// sudoValidate refreshes the sudo timestamp with the given password.
func sudoValidate(ctx context.Context, password []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var output tailBuffer
	cmd := exec.CommandContext(ctx, "sudo", "-S", "-v", "-p", "")
	cmd.Stdin = strings.NewReader(string(password) + "\n")
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	if err != nil {
		text := strings.TrimSpace(clean(output.String()))
		if text == "" {
			text = err.Error()
		}
		return &sudoError{firstLine(text)}
	}
	return nil
}

type sudoError struct{ text string }

func (e *sudoError) Error() string { return e.text }

// ensureSudo runs then once sudo credentials are available, asking for the
// password in the workspace when they are not.
func (w *workspace) ensureSudo(then func()) {
	go func() {
		cached := sudoCached(w.ctx)
		w.queue(func() {
			if cached {
				then()
				return
			}
			w.sudoPasswordDialog("", then)
		})
	}()
}

func (w *workspace) sudoPasswordDialog(problem string, then func()) {
	focus := w.app.GetFocus()
	p := w.palette()
	name := "your account"
	if current, err := user.Current(); err == nil && current.Username != "" {
		name = current.Username
	}
	field := tview.NewInputField().SetLabel(" Password ").SetMaskCharacter('•').SetFieldWidth(40)
	field.SetLabelColor(tcell.GetColor(p.accent)).SetFieldBackgroundColor(tcell.GetColor(p.background)).SetFieldTextColor(tcell.GetColor(p.text))
	note := textView().SetDynamicColors(true).SetWrap(true)
	text := "[" + p.muted + "]sudo has no cached authorization. The password goes to sudo -S -v on standard input, refreshes its timestamp and is discarded; nothing is stored. Enter continues · Escape cancels.[-]"
	if problem != "" {
		text = "[" + p.error + "::b]" + tview.Escape(clean(problem)) + "[-::-]\n" + text
	}
	note.SetText(text)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(note, 0, 1, false).AddItem(field, 1, 0, true)
	panel.SetBackgroundColor(tcell.GetColor(p.surface)).SetBorder(true).SetBorderColor(tcell.GetColor(p.warning)).SetBorderAttributes(tcell.AttrBold)
	panel.SetTitle(" " + w.iconLabel(iconLock, "AUTHORIZE · sudo for "+tview.Escape(clean(name))+" ")).SetTitleColor(tcell.GetColor(p.warning))
	close := func() { w.pages.RemovePage("sudo"); w.app.SetFocus(focus) }
	field.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			close()
			return
		}
		if key != tcell.KeyEnter {
			return
		}
		password := []byte(field.GetText())
		field.SetText("")
		close()
		go func() {
			err := sudoValidate(w.ctx, password)
			for i := range password {
				password[i] = 0
			}
			w.queue(func() {
				if err != nil {
					w.sudoPasswordDialog(err.Error(), then)
					return
				}
				then()
			})
		}()
	})
	w.pages.AddPage("sudo", centeredDialog(panel, 84, 8), true, true)
	w.app.SetFocus(field)
}
