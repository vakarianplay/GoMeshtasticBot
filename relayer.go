package main

import (
	"context"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// const (
// 	matrixHomeserver = "https://matrix.raspad.space"
// 	matrixUsername   = "meshrelayer"
// 	matrixPassword   = "aczBRERO3LcOMff"
// 	matrixDeviceID   = "MESHBOT"
// 	matrixRoomID     = "!mSmjqRsxficDk2Xxw6:matrix.raspad.space"

// 	telegramBotToken       = "8616957483:AAHW4t-HAj06dvf5YxcF_9JDvGmA_XH7Roo"
// 	telegramChatID   int64 = -5237468793
// )

var relayConfig relayCfg

type relayCfg struct {
	matrixHomeserver string
	matrixUsername   string
	matrixPassword   string
	matrixDeviceID   string
	matrixRoomID     string
	enabledMatrix    string
	telegramBotToken string
	telegramChatID   string
	enabledTelegram  string
}

var tgFlag bool
var matrixFlag bool

func configRelay() {
	relayConfig.matrixHomeserver, relayConfig.matrixUsername, relayConfig.matrixPassword, relayConfig.matrixDeviceID, relayConfig.matrixRoomID, relayConfig.enabledMatrix = GetMatrixConfig()
	relayConfig.telegramBotToken, relayConfig.telegramChatID, relayConfig.enabledTelegram = GetTelegramConfig()
	matrixFlag, _ = strconv.ParseBool(relayConfig.enabledMatrix)
	tgFlag, _ = strconv.ParseBool(relayConfig.enabledTelegram)
}

func RelayMesaage(message string) {
	configRelay()
	sendToMatrix(message)
	sendToTelegram(message)
}

func sendToMatrix(message string) error {
	ctx := context.Background()

	c, err := mautrix.NewClient(relayConfig.matrixHomeserver, "", "")
	if err != nil {
		return err
	}

	login, err := c.Login(ctx, &mautrix.ReqLogin{
		Type: "m.login.password",
		Identifier: mautrix.UserIdentifier{
			Type: "m.id.user",
			User: relayConfig.matrixUsername,
		},
		Password: relayConfig.matrixPassword,
		DeviceID: id.DeviceID(relayConfig.matrixDeviceID),
	})
	if err != nil {
		return err
	}
	c.SetCredentials(login.UserID, login.AccessToken)

	_, err = c.SendMessageEvent(
		ctx,
		id.RoomID(relayConfig.matrixRoomID),
		event.EventMessage,
		&event.MessageEventContent{
			MsgType: event.MsgText,
			Body:    message,
		},
	)
	return err
}

func sendToTelegram(message string) error {
	form := url.Values{}
	form.Set("chat_id", relayConfig.telegramChatID)
	form.Set("text", message)

	apiURL := fmt.Sprintf(
		"https://api.telegram.org/bot%s/sendMessage", relayConfig.telegramBotToken)

	resp, err := http.PostForm(apiURL, form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram api status: %s", resp.Status)
	}

	return nil
}
