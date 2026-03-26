package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	nekosql "neko_sql"
)

type sqlGUI struct {
	app    fyne.App
	window fyne.Window

	addrEntry   *widget.Entry
	statusLabel *widget.Label
	queryEntry  *widget.Entry
	outputEntry *widget.Entry

	client *nekosql.Client
}

func main() {
	gui := &sqlGUI{}
	gui.app = fyneapp.NewWithID("neko_sql_gui")
	gui.window = gui.app.NewWindow("neko_sql GUI")
	gui.window.Resize(fyne.NewSize(1180, 780))
	gui.window.SetContent(gui.build())
	gui.window.ShowAndRun()
}

func (g *sqlGUI) build() fyne.CanvasObject {
	g.addrEntry = widget.NewEntry()
	g.addrEntry.SetText("127.0.0.1:17401")

	g.statusLabel = widget.NewLabel("Disconnected")

	g.queryEntry = widget.NewMultiLineEntry()
	g.queryEntry.Wrapping = fyne.TextWrapWord
	g.queryEntry.SetMinRowsVisible(10)
	g.queryEntry.SetText("SELECT * FROM players;")

	g.outputEntry = widget.NewMultiLineEntry()
	g.outputEntry.Wrapping = fyne.TextWrapWord
	g.outputEntry.Disable()
	g.outputEntry.SetMinRowsVisible(18)

	connectBtn := widget.NewButton("Connect", func() { go g.connect() })
	disconnectBtn := widget.NewButton("Disconnect", func() { g.disconnect() })
	runBtn := widget.NewButton("Run SQL", func() { go g.execQuery(strings.TrimSpace(g.queryEntry.Text)) })
	beginBtn := widget.NewButton("BEGIN", func() { go g.execQuery("BEGIN") })
	commitBtn := widget.NewButton("COMMIT", func() { go g.execQuery("COMMIT") })
	rollbackBtn := widget.NewButton("ROLLBACK", func() { go g.execQuery("ROLLBACK") })
	seedBtn := widget.NewButton("Seed Demo", func() {
		g.queryEntry.SetText(strings.Join([]string{
			"INSERT INTO players (id, name, mmr) VALUES (1, 'alice', 1200);",
			"INSERT INTO players (id, name, mmr) VALUES (2, 'bob', 1150);",
			"SELECT * FROM players;",
		}, "\n"))
	})

	left := container.NewBorder(
		container.NewVBox(
			widget.NewLabel("Server"),
			g.addrEntry,
			container.NewGridWithColumns(2, connectBtn, disconnectBtn),
			widget.NewSeparator(),
			widget.NewLabel("SQL"),
		),
		container.NewVBox(
			container.NewGridWithColumns(2, beginBtn, commitBtn),
			container.NewGridWithColumns(2, rollbackBtn, seedBtn),
			runBtn,
		),
		nil,
		nil,
		g.queryEntry,
	)

	right := container.NewBorder(
		container.NewVBox(
			widget.NewLabel("Status"),
			g.statusLabel,
			widget.NewSeparator(),
			widget.NewLabel("Result"),
		),
		nil,
		nil,
		nil,
		g.outputEntry,
	)

	return container.NewBorder(
		container.NewVBox(widget.NewLabelWithStyle("neko_sql GUI", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})),
		nil,
		nil,
		nil,
		container.NewHSplit(left, right),
	)
}

func (g *sqlGUI) connect() {
	g.setStatus("Connecting...")
	client, err := nekosql.Dial(strings.TrimSpace(g.addrEntry.Text), 3*time.Second)
	if err != nil {
		g.showError(err)
		g.setStatus("Connect failed")
		return
	}
	if g.client != nil {
		_ = g.client.Close()
	}
	g.client = client
	g.setStatus("Connected")
}

func (g *sqlGUI) disconnect() {
	if g.client != nil {
		_ = g.client.Close()
		g.client = nil
	}
	g.setStatus("Disconnected")
}

func (g *sqlGUI) execQuery(sql string) {
	if sql == "" {
		g.showError(fmt.Errorf("sql is empty"))
		return
	}
	if g.client == nil {
		g.showError(fmt.Errorf("connect first"))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := g.client.Exec(ctx, sql)
	if err != nil {
		g.showError(err)
		return
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	fyne.Do(func() {
		g.outputEntry.SetText(string(data))
	})
	g.setStatus("Last query OK")
}

func (g *sqlGUI) setStatus(text string) {
	fyne.Do(func() {
		g.statusLabel.SetText(text)
	})
}

func (g *sqlGUI) showError(err error) {
	fyne.Do(func() {
		dialog.ShowError(err, g.window)
	})
}
