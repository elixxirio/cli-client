package ui

import (
	"fmt"
	"github.com/jroimartin/gocui"
	"github.com/pkg/errors"
	jww "github.com/spf13/jwalterweatherman"
	"gitlab.com/xx_network/primitives/id"
	"strings"
	"time"
)

const (
	titleBox     = "titleBox"
	channelFeed  = "channelFeed"
	messageInput = "messageInputBox"
	sendButton   = "sendButtonBox"
	messageCount = "messageCountBox"
)

const charCountFmt = "%d/\n%d"

var (
	viewArr = []string{channelFeed, messageInput, sendButton}
	active  = 0
)

func MakeUI(payloadChan chan []byte, broadcastFn func(message []byte) error,
	channelName, username, description string, receptionID *id.ID,
	maxMessageLen int) {
	g, err := gocui.NewGui(gocui.Output256)
	if err != nil {
		jww.FATAL.Panicf("Failed to make new GUI: %+v", err)
	}
	defer g.Close()

	g.Cursor = true
	g.Mouse = true
	g.SelFgColor = gocui.ColorGreen
	g.Highlight = true

	g.SetManagerFunc(makeLayout(channelName, username, description, receptionID, maxMessageLen))

	err = initKeybindings(g, broadcastFn, maxMessageLen)
	if err != nil {
		jww.FATAL.Panicf("Failed to generate key bindings: %+v", err)
	}
	go func() {
		for {
			select {
			case r := <-payloadChan:
				channelFeedView, err := g.View(channelFeed)
				if err != nil {
					jww.ERROR.Print(err)
				}

				payloadParts := strings.SplitN(string(r), ":\xb1", 2)
				username := "\x1b[38;5;255m" + string(payloadParts[0]) + "\x1b[0m \u001B[38;5;245m[received " + time.Now().Format("3:04:05 pm") + "]\u001B[0m"
				message := "\x1b[38;5;250m" + strings.TrimSpace(payloadParts[1]) + "\x1b[0m"

				str := username + "\n" + message + "\n\n"

				_, err = fmt.Fprintf(channelFeedView, str)
				if err != nil {
					jww.ERROR.Print(err)
					return
				}

				channelFeedView.Autoscroll = true
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		for {
			select {
			case <-ticker.C:
				g.Update(func(gui *gocui.Gui) error {
					messageInputView, err := g.View(messageInput)
					if err != nil {
						return errors.Errorf("Failed to get view: %+v", err)
					}
					messageCountView, err := g.View(messageCount)
					if err != nil {
						return errors.Errorf("Failed to get view: %+v", err)
					}

					buff := strings.TrimSpace(messageInputView.Buffer())
					n := len(buff)

					messageCountView.Clear()
					_, err = fmt.Fprintf(messageCountView, charCountFmt, n, maxMessageLen)
					if err != nil {
						return errors.Errorf("Failed to write to view: %+v", err)
					}
					return nil
				})
			}
		}
	}()

	if err = g.MainLoop(); err != nil && err != gocui.ErrQuit {
		jww.FATAL.Panicf("Error in main loop: %+v", err)
	}
}

func makeLayout(channelName, username, description string, receptionID *id.ID, maxMessageLen int) func(g *gocui.Gui) error {
	return func(g *gocui.Gui) error {
		maxX, maxY := g.Size()

		if v, err := g.SetView(titleBox, maxX-25, 0, maxX-1, maxY-7); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			v.Title = " xx Channel Chat "
			v.Wrap = true

			_, err = fmt.Fprintf(v, "Controls:\n"+
				"\u001B[38;5;250m"+
				" Ctrl+C  exit\n"+
				" Tab     Switch view\n"+
				" ↑ ↓     Seek input\n"+
				" Enter   Send message\n"+
				" Ctrl+Enter New Line\n"+
				" F4      Channel feed\n"+
				" F5      Message field\n\n"+
				"\x1b[0m"+
				"Channel Info:\n"+
				" \u001B[38;5;252mName:\u001B[0m \u001B[38;5;250m"+channelName+"\u001B[0m\n"+
				" \u001B[38;5;252mDescription:\u001B[0m \u001B[38;5;250m"+description+"\u001B[0m\n"+
				" \u001B[38;5;252mID:\u001B[0m \u001B[38;5;250m"+receptionID.String()+"\u001B[0m")
			if err != nil {
				return err
			}
		}

		if v, err := g.SetView(channelFeed, 0, 0, maxX-26, maxY-7); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			v.Title = " Channel Feed for \"" + channelName + "\" [F4] "
			v.Wrap = true
			v.Autoscroll = true
		}

		if v, err := g.SetView(messageInput, 0, maxY-6, maxX-9, maxY-1); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			v.Title = " Sending Message as \"" + username + "\" [F5] "
			v.Editable = true
			v.Wrap = true

			if _, err = g.SetCurrentView(messageInput); err != nil {
				return err
			}
		}

		if v, err := g.SetView(messageCount, maxX-8, maxY-6, maxX-1, maxY-3); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			v.Frame = false
			v.Wrap = true

			_, err = fmt.Fprintf(v, charCountFmt, 0, maxMessageLen)
			if err != nil {
				return err
			}
		}

		if v, err := g.SetView(sendButton, maxX-8, maxY-3, maxX-1, maxY-1); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}

			v.Highlight = true

			_, err = v.Write([]byte("\n Send "))
			if err != nil {
				return err
			}
		}
		return nil
	}
}

// initKeybindings initializes all key bindings for the entire UI.
func initKeybindings(g *gocui.Gui, broadcastFn func(message []byte) error, maxMessageLen int) error {
	err := g.SetKeybinding("", gocui.KeyTab, gocui.ModNone, nextView)
	if err != nil {
		return errors.Errorf(
			"failed to set key binding for tab: %+v", err)
	}

	err = g.SetKeybinding(
		messageInput, gocui.KeyEnter, gocui.ModNone, readBuffs(broadcastFn, maxMessageLen))
	if err != nil {
		return errors.Errorf(
			"failed to set key binding for enter: %+v", err)
	}

	err = g.SetKeybinding(
		sendButton, gocui.KeyEnter, gocui.ModNone, readBuffs(broadcastFn, maxMessageLen))
	if err != nil {
		return errors.Errorf(
			"failed to set key binding for enter: %+v", err)
	}

	err = g.SetKeybinding(
		sendButton, gocui.MouseLeft, gocui.ModNone, readBuffs(broadcastFn, maxMessageLen))
	if err != nil {
		return errors.Errorf(
			"failed to set key binding for enter: %+v", err)
	}

	err = g.SetKeybinding(
		messageInput, gocui.KeyCtrlJ, gocui.ModNone, addLine)
	if err != nil {
		return errors.Errorf(
			"failed to set key binding for enter: %+v", err)
	}

	err = g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, quit)
	if err != nil {
		return errors.Errorf(
			"failed to set key binding for Ctrl + C: %+v", err)
	}

	err = g.SetKeybinding("", gocui.KeyF4, gocui.ModNone, switchActiveTo(channelFeed))
	if err != nil {
		return errors.Errorf(
			"failed to set key binding for F4: %+v", err)
	}

	err = g.SetKeybinding("", gocui.KeyF5, gocui.ModNone, switchActiveTo(messageInput))
	if err != nil {
		return errors.Errorf(
			"failed to set key binding for F5: %+v", err)
	}

	for _, v := range viewArr {
		err = g.SetKeybinding(v, gocui.KeyArrowUp, gocui.ModNone, scrollView(-1))
		if err != nil {
			return errors.Errorf(
				"failed to set key binding for arrow up: %+v", err)
		}

		err = g.SetKeybinding(v, gocui.MouseWheelUp, gocui.ModNone, scrollView(-1))
		if err != nil {
			return errors.Errorf(
				"failed to set key binding for wheel up: %+v", err)
		}

		err = g.SetKeybinding(v, gocui.KeyArrowDown, gocui.ModNone, scrollView(1))
		if err != nil {
			return errors.Errorf(
				"failed to set key binding for arrow down: %+v", err)
		}

		err = g.SetKeybinding(v, gocui.MouseWheelDown, gocui.ModNone, scrollView(1))
		if err != nil {
			return errors.Errorf(
				"failed to set key binding for wheel down: %+v", err)
		}

		err = g.SetKeybinding(v, gocui.MouseLeft, gocui.ModNone, switchActive)
		if err != nil {
			return errors.Errorf(
				"failed to set key binding for left mouse button: %+v", err)
		}
	}

	return nil
}

func switchActive(g *gocui.Gui, v *gocui.View) error {
	jww.TRACE.Printf("Set current view to %s", v.Name())
	if _, err := g.SetCurrentView(v.Name()); err != nil {
		return errors.Errorf(
			"failed to set %s as current view: %+v", v.Name(), err)
	}
	if v.Name() == messageInput {
		g.Cursor = true
	} else {
		g.Cursor = false
	}
	return nil
}

func switchActiveTo(name string) func(g *gocui.Gui, v *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		jww.TRACE.Printf("Set current view to %s", name)
		if _, err := g.SetCurrentView(name); err != nil {
			return errors.Errorf(
				"failed to set %s as current view: %+v", name, err)
		}
		if name == messageInput {
			g.Cursor = true
		} else {
			g.Cursor = false
		}
		return nil
	}
}

func readBuffs(broadcastFn func(message []byte) error, maxMessageLen int) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, _ *gocui.View) error {
		v, err := g.View(messageInput)
		if err != nil {
			return err
		}
		buff := strings.TrimSpace(v.Buffer())

		if len(buff) == 0 || len(buff) > maxMessageLen {
			return nil
		}

		err = broadcastFn([]byte(buff))
		if err != nil {
			return err
		}

		v.Clear()
		err = v.SetOrigin(0, 0)
		if err != nil {
			return errors.Errorf("Failed to set origin back to (0, 0): %+v", err)
		}
		err = v.SetCursor(0, 0)
		if err != nil {
			return errors.Errorf("Failed to set cursor back to (0, 0): %+v", err)
		}

		messageCountView, err := g.View(messageCount)
		if err != nil {
			return errors.Errorf("Failed to get view: %+v", err)
		}

		messageCountView.Clear()
		_, err = fmt.Fprintf(messageCountView, charCountFmt, 0, maxMessageLen)
		if err != nil {
			return errors.Errorf("Failed to write to view: %+v", err)
		}

		return nil
	}
}

func addLine(g *gocui.Gui, v *gocui.View) error {
	v.EditNewLine()
	return nil
}

func scrollView(dy int) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		if v != nil && g.CurrentView() == v {
			v.Autoscroll = false
			ox, oy := v.Origin()
			_, height := v.Size()

			if (dy+oy >= 0) && (dy+oy+height <= len(v.ViewBufferLines())) {
				if err := v.SetOrigin(ox, oy+dy); err != nil {
					return err
				}
			}
		}
		return nil
	}
}

func nextView(g *gocui.Gui, v *gocui.View) error {
	nextIndex := (active + 1) % len(viewArr)
	name := viewArr[nextIndex]

	_, err := g.View(name)
	if err != nil {
		return err
	}

	if _, err := setCurrentViewOnTop(g, name); err != nil {
		return err
	}

	if v.Name() == messageInput {
		g.Cursor = true
	} else {
		g.Cursor = false
	}

	active = nextIndex
	return nil
}

func setCurrentViewOnTop(g *gocui.Gui, name string) (*gocui.View, error) {
	if _, err := g.SetCurrentView(name); err != nil {
		return nil, err
	}
	return g.SetViewOnTop(name)
}

func quit(*gocui.Gui, *gocui.View) error {
	return gocui.ErrQuit
}