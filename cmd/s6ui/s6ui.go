//nolint:forbidigo
package main

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/hpcloud/tail"
	"github.com/rivo/tview"
	"mko.re/s6ui"
)

//go:embed help.txt
var helpText string

var keyToSignal = map[rune]syscall.Signal{
	'A': syscall.SIGALRM,
	'B': syscall.SIGABRT,
	'Q': syscall.SIGQUIT,
	'H': syscall.SIGHUP,
	'K': syscall.SIGKILL,
	'T': syscall.SIGTERM,
	'I': syscall.SIGINT,
	'1': syscall.SIGUSR1,
	'2': syscall.SIGUSR2,
	'P': syscall.SIGSTOP,
	'C': syscall.SIGCONT,
	'Y': syscall.SIGWINCH,
}

func run() error {
	if len(os.Args) != 2 {
		fmt.Printf("Usage: %s <directory>", os.Args[0])
		return nil
	}
	targetDir := os.Args[1]

	ctx := context.Background()

	s6 := s6ui.S6{Dir: targetDir}

	services, err := s6.ListServices()
	if err != nil {
		return err
	}

	app := tview.NewApplication()
	app.EnableMouse(true)

	var pages *tview.Pages

	list := tview.NewList().ShowSecondaryText(false).SetHighlightFullLine(true).SetSelectedBackgroundColor(tcell.ColorGray)
	list.SetBorder(true)
	list.SetTitle("Services")
	for _, svc := range services {
		list.AddItem(svc.Name(), "", 0, nil)
	}

	update := func() {
		lines := make([]string, 0, len(services))
		for _, svc := range services {
			stat, err := svc.Stat()
			var line strings.Builder
			if err != nil {
				line.WriteString("[red]×[white]")
			} else if stat.Up {
				if stat.WantedUp {
					line.WriteString("[green]")
				} else {
					line.WriteString("[orange]")
				}
				line.WriteString("↑[white]")
			} else {
				if stat.WantedUp {
					line.WriteString("[red]")
				} else {
					line.WriteString("[gray]")
				}
				line.WriteString("↓[white]")
			}
			line.WriteString(" ")
			if stat.Ready {
				line.WriteString("[green]✓[white]")
			} else {
				line.WriteString(" ")
			}
			line.WriteString(" ")

			line.WriteString(svc.Name())
			if err != nil {
				line.WriteString(" - ")
				line.WriteString(fmt.Sprintf("[red]error: %s[white]", err))
			} else {
				if !stat.Up && stat.WantedUp {
					line.WriteString(" - ")
					line.WriteString(fmt.Sprintf("[red]exitcode: %d - ", stat.ExitCode))
					line.WriteString(fmt.Sprintf("signal: %s - ", stat.Signal))
					line.WriteString("[white]")
				}
			}
			lines = append(lines, line.String())
		}
		app.QueueUpdateDraw(func() {
			for i, line := range lines {
				list.SetItemText(i, line, "")
			}
		})
	}

	go func() {
		for {
			update()
			time.Sleep(1 * time.Second)
		}
	}()

	getSelectedService := func() *s6ui.Service {
		selectedItem := list.GetCurrentItem()
		if selectedItem >= 0 {
			return services[selectedItem]
		}
		return nil
	}

	logV := tview.NewTextView()
	logV.SetBorder(true)
	logV.SetDynamicColors(true)

	////////////////////////////////////////////////////////////////////////////////

	var cleanup []*tail.Tail
	var logT *tail.Tail
	var logW io.Writer
	logViewVisible := false
	loadLog := func(svci int) {
		logV.Clear()
		if logT != nil {
			_ = logT.Stop()
			cleanup = append(cleanup, logT)
		}
		svc := services[svci]
		logV.SetTitle(fmt.Sprintf("%s (log)", svc.Name()))
		logT, err = svc.OpenLog()
		if err != nil {
			logT = nil
			_, _ = logV.Write([]byte(tview.Escape(fmt.Sprintf("[red]Error opening log: %v[white]\n", err))))
			return
		}
		logT.Logger = tail.DiscardingLogger
		logW = tview.ANSIWriter(logV)
		logV.ScrollToEnd()
		go func() {
			for line := range logT.Lines {
				app.QueueUpdateDraw(func() {
					_, _ = logW.Write([]byte(colorizeLog(tview.Escape(line.Text)) + "\n"))
				})
			}
		}()
	}

	////////////////////////////////////////////////////////////////////////////////
	// Help
	////////////////////////////////////////////////////////////////////////////////

	helpLines := strings.Split(helpText, "\n")

	// AddButtons([]string{"Close"}).
	// SetDoneFunc(func(buttonIndex int, buttonLabel string) {
	// 	pages.HidePage("help")
	// })

	helpV := tview.NewTextView()
	helpV.SetTitle("Help")
	helpV.SetTitleColor(tcell.ColorYellow)
	helpV.SetBackgroundColor(tcell.ColorDarkBlue)
	helpV.SetText(helpText)
	helpV.SetDynamicColors(true)
	helpV.SetBorder(true)

	helpModal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(helpV, len(helpLines)+2, 1, true).
			AddItem(nil, 0, 1, false), 48, 1, true).
		AddItem(nil, 0, 1, false)
	helpModal.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc || event.Key() == tcell.KeyEnter || (event.Key() == tcell.KeyRune && event.Rune() == 'q') {
			pages.HidePage("help")
			return nil
		} else if event.Key() == tcell.KeyCtrlL {
			app.Sync()
			return nil
		}
		return event
	})

	////////////////////////////////////////////////////////////////////////////////

	flex := tview.NewFlex().
		AddItem(list, 0, 1, true)

	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		//nolint:exhaustive
		switch event.Key() {
		case tcell.KeyEnter:
			logViewVisible = !logViewVisible
			if logViewVisible {
				flex.AddItem(logV, 0, 3, false)
				loadLog(list.GetCurrentItem())
			} else {
				flex.RemoveItem(logV)
				if logT != nil {
					_ = logT.Stop()
					cleanup = append(cleanup, logT)
					logT = nil
				}
			}
			return nil
		case tcell.KeyCtrlL:
			app.Sync()
			return nil
		case tcell.KeyHome, tcell.KeyCtrlA:
			logV.ScrollToBeginning()
			return nil
		case tcell.KeyEnd, tcell.KeyCtrlE:
			logV.ScrollToEnd()
			return nil
		case tcell.KeyPgUp, tcell.KeyCtrlU:
			_, _, _, height := logV.GetInnerRect()
			row, _ := logV.GetScrollOffset()
			logV.ScrollTo(row-height, 0)
			return nil
		case tcell.KeyPgDn, tcell.KeyCtrlD:
			_, _, _, height := logV.GetInnerRect()
			row, _ := logV.GetScrollOffset()
			logV.ScrollTo(row+height, 0)
			return nil
		case tcell.KeyRune:
			signal, ok := keyToSignal[event.Rune()]
			if ok {
				if svc := getSelectedService(); svc != nil {
					if err := svc.Signal(ctx, signal); err != nil {
						log.Printf("error sending terminate signal: %v", err)
					}
					go update()
				}
				return nil
			}

			switch event.Rune() {
			case '?':
				pages.ShowPage("help")
				return nil
			case 'j':
				return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
			case 'k':
				return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
			case 'u':
				if svc := getSelectedService(); svc != nil {
					if err := svc.Up(ctx); err != nil {
						log.Printf("error requesting up: %v", err)
					}
					go update()
				}
				return nil
			case 'd':
				if svc := getSelectedService(); svc != nil {
					if err := svc.Down(ctx); err != nil {
						log.Printf("error requesting down: %v", err)
					}
					go update()
				}
				return nil
			case 'r':
				if svc := getSelectedService(); svc != nil {
					if err := svc.Restart(ctx); err != nil {
						log.Printf("error requesting restart: %v", err)
					}
					go update()
				}
				return nil
			case 'q':
				app.Stop()
				return nil
			}
		}
		return event
	})
	list.SetChangedFunc(func(index int, mainText string, secondaryText string, shortcut rune) {
		if logViewVisible {
			loadLog(index)
		}
	})

	////////////////////////////////////////////////////////////////////////////////

	frame := tview.NewFrame(flex).SetBorders(0, 0, 0, 0, 0, 0)
	frame.AddText("?: Help", false, tview.AlignCenter, tcell.ColorGreen)

	pages = tview.NewPages()
	pages.AddPage("main", frame, true, true)
	pages.AddPage("help", helpModal, true, false)

	if err := app.SetRoot(pages, true).Run(); err != nil {
		return err
	}

	for _, t := range cleanup {
		t.Cleanup()
	}

	return nil
}

var timeRE = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d+)`)

func colorizeLog(s string) string {
	return timeRE.ReplaceAllString(s, `[gray]$1[-]`)
}

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}
