package tray

type trayMenuItem struct {
	Label  string
	Action Action
}

func trayMenuItems() []trayMenuItem {
	return []trayMenuItem{
		{Label: "Open text chat", Action: ActionOpenTextChat},
		{Label: "Open voice chat / Show voice chat", Action: ActionOpenOrShowVoice},
		{Label: "Minimize voice to tray", Action: ActionMinimizeVoice},
		{Label: "Configure shortcuts", Action: ActionConfigureShort},
		{Label: "Quit DuckChat", Action: ActionQuitService},
	}
}

func handleTrayIconClick(dispatch TrayActionDispatcher) Response {
	if dispatch == nil {
		return Response{Error: "tray action dispatcher is unavailable"}
	}
	status := dispatch(ActionVoiceStatus)
	if status.Error != "" {
		return status
	}
	if status.VoiceActive {
		return dispatch(ActionToggleVoice)
	}
	return dispatch(ActionOpenTextChat)
}
