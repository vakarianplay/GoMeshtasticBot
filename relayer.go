package main

import (
	"context"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

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
