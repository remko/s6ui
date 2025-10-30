///usr/bin/true; exec /usr/bin/env go run "$0" "$@".

//nolint:forbidigo
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

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

	s6 := S6{Dir: targetDir}

	services, err := s6.ListServices()
	if err != nil {
		return err
	}

	app := tview.NewApplication()
	list := tview.NewList().ShowSecondaryText(false).SetHighlightFullLine(true).SetSelectedBackgroundColor(tcell.ColorGray)
	list.SetBorder(true)
	list.SetTitle(targetDir)
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

	getSelectedService := func() *Service {
		selectedItem := list.GetCurrentItem()
		if selectedItem >= 0 {
			return services[selectedItem]
		}
		return nil
	}

	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		//nolint:exhaustive
		switch event.Key() {
		case tcell.KeyCtrlT:
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

	if err := app.SetRoot(list, true).Run(); err != nil {
		return err
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}
