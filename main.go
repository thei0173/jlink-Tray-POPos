package main

import "fyne.io/systray"

func main() {
	systray.Run(onTrayReady, onTrayExit)
}
