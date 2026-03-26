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
	"strings"
)

var relayConfig relayCfg

type relayCfg struct {
	matrixHomeserver  string
	matrixUsername    string
	matrixPassword    string
	matrixDeviceID    string
	matrixRoomID      string
	enabledMatrix     string
	telegramBotToken  string
	telegramChatID    string
	enabledTelegram   string
	customServerURL   string
	customServerToken string
	enabledCustom     string
}

var tgFlag bool
var matrixFlag bool
var customFlag bool

func configRelay() {
	relayConfig.matrixHomeserver, relayConfig.matrixUsername, relayConfig.matrixPassword, relayConfig.matrixDeviceID, relayConfig.matrixRoomID, relayConfig.enabledMatrix = GetMatrixConfig()
	relayConfig.telegramBotToken, relayConfig.telegramChatID, relayConfig.enabledTelegram = GetTelegramConfig()
	relayConfig.enabledCustom, relayConfig.customServerURL, relayConfig.customServerToken = GetCustomRelayConfig()
	matrixFlag, _ = strconv.ParseBool(relayConfig.enabledMatrix)
	tgFlag, _ = strconv.ParseBool(relayConfig.enabledTelegram)
	customFlag, _ = strconv.ParseBool(relayConfig.enabledCustom)

	fmt.Println(relayConfig.customServerToken)
}

func RelayMesaage(message string) {
	configRelay()
	sendToCustomServer(message)

	if matrixFlag {
		sendToMatrix(message)
	}
	if tgFlag {
		sendToTelegram(message)
	}

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

func sendToCustomServer(message string) error {
	req, err := http.NewRequest(http.MethodPost, relayConfig.customServerURL, strings.NewReader(message))
	if err != nil {
		return err
	}

	fmt.Println(relayConfig.customServerURL, relayConfig.customServerToken, message)

	req.Header.Set("X-Auth-Token", relayConfig.customServerToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("custom server status: %s", resp.Status)
	}
	return nil
}
