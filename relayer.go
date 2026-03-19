package main

import (
	"context"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func relayMesaage(message string) {
	sendToMatrix(message)
	sendToTelegram(message)
}

func sendToMatrix(message string) error {
	ctx := context.Background()

	c, err := mautrix.NewClient(matrixHomeserver, "", "")
	if err != nil {
		return err
	}

	login, err := c.Login(ctx, &mautrix.ReqLogin{
		Type: "m.login.password",
		Identifier: mautrix.UserIdentifier{
			Type: "m.id.user",
			User: matrixUsername,
		},
		Password: matrixPassword,
		DeviceID: id.DeviceID(matrixDeviceID),
	})
	if err != nil {
		return err
	}
	c.SetCredentials(login.UserID, login.AccessToken)

	_, err = c.SendMessageEvent(
		ctx,
		id.RoomID(matrixRoomID),
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
	form.Set("chat_id", strconv.FormatInt(telegramChatID, 10))
	form.Set("text", message)

	apiURL := fmt.Sprintf(
		"https://api.telegram.org/bot%s/sendMessage",
		telegramBotToken,
	)

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
