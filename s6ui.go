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
	list := tview.NewList().ShowSecondaryText(false).SetHighlightFullLine(true)
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
			line.WriteString(svc.Name() + " - ")
			if err != nil {
				line.WriteString(fmt.Sprintf("[red]error: %s[white]", err))
			} else {
				if stat.Up {
					line.WriteString("[green]up[white] - ")
					line.WriteString(fmt.Sprintf("[grey]pid: %d[white] - ", stat.Pid))
				} else {
					line.WriteString("[red]down[white] - ")
					line.WriteString(fmt.Sprintf("exitcode: %d - ", stat.ExitCode))
					line.WriteString(fmt.Sprintf("signal: %s - ", stat.Signal))
				}
				line.WriteString(stat.UpdownFor.String())
				if stat.Ready {
					line.WriteString("  [green]ready[white] - ")
					line.WriteString(stat.ReadyFor.String())
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

	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		//nolint:exhaustive
		switch event.Key() {
		case tcell.KeyCtrlT:
			selectedItem := list.GetCurrentItem()
			if selectedItem >= 0 {
				err := services[selectedItem].Signal(ctx, syscall.SIGTERM)
				if err != nil {
					log.Printf("error sending terminate signal: %v", err)
				}
				go update()
			}
			return nil
		case tcell.KeyRune:
			switch event.Rune() {
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
