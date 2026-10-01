package main

import (
	"embed"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"runbook/runbook"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	home, _ := os.UserHomeDir()
	app := NewApp(&runbook.Store{Root: filepath.Join(home, "runbooks"), TrashDir: filepath.Join(home, ".Trash")})

	err := wails.Run(&options.App{
		Title:  "Runbook",
		Width:  1200,
		Height: 820,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 246, G: 246, B: 245, A: 1},
		Menu:             app.appMenu(),
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
