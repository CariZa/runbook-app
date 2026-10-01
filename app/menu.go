package main

import (
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ZoomEventName asks the frontend to change its zoom: "in", "out" or "reset".
const ZoomEventName = "zoom"

// appMenu builds the macOS menu bar. Besides View (zoom), it adds the standard App and Edit
// menus: without an Edit menu, macOS gives the webview no ⌘C / ⌘V / ⌘A.
func (a *App) appMenu() *menu.Menu {
	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	m.Append(menu.EditMenu())

	view := m.AddSubmenu("View")
	view.AddText("Zoom In", keys.CmdOrCtrl("+"), a.zoom("in"))
	view.AddText("Zoom Out", keys.CmdOrCtrl("-"), a.zoom("out"))
	view.AddText("Actual Size", keys.CmdOrCtrl("0"), a.zoom("reset"))
	return m
}

func (a *App) zoom(what string) func(*menu.CallbackData) {
	return func(*menu.CallbackData) { runtime.EventsEmit(a.ctx, ZoomEventName, what) }
}
