package main

import i "github.com/dinhdev-nu/chat-platform-api/internal/initialize"

func main() {

	app := i.NewAPIApp()
	app.Run()

}
