# GoMeshtasticBot <img src="https://github.com/meshtastic/firmware/raw/develop/.github/meshtastic_logo.png" alt="Meshtastic Logo" width="80"/>

![alt text](https://img.shields.io/badge/Golang-1.21.1-blue?style=flat-square&logo=go)
![alt text](https://img.shields.io/badge/Telegram%20integration-gray?style=flat-square&logo=telegram)
![alt text](https://img.shields.io/badge/Matrix%20integration-gray?style=flat-square&logo=matrix)


![alt text](https://img.shields.io/badge/Status-in%20complete-2E8B57?style=for-the-badge&logo=Buddy)

### Ping and informer bot for meshtastic node in one execute file


<img width="300" alt="image_2026-03-24_08-08-18" src="https://github.com/user-attachments/assets/cbedc4ff-e92c-4513-9fb3-f1d14e96933c" />
<img width="300" alt="image_2026-03-24_08-08-15" src="https://github.com/user-attachments/assets/94592aeb-f8ce-4e4c-a115-b4eb2d7f0adf" />

-------------------------

## 🛠️ Releases: 

> For windows x64 and linux x64, armv7, arm64: [https://github.com/vakarianplay/Gosling_tgbot/releases](https://github.com/vakarianplay/GoMeshtasticBot/releases/tag/v1.0)


## 💎 Features

>* Collect info about neighbours
>* Pinger for broadcast-channels and for DM
>* Weather info
>* Currency rates info
>* Integration with narodmon.ru
>* Relay messages from radio to telegtam group
>* Relay messages to matrix room (without enctyprion)
>* Auto trying to reconnect 


## 🚀 How to start

>* [Download release for your platform](https://github.com/vakarianplay/GoMeshtasticBot/releases/tag/v1.0)
>* Connect your meshtastic node to wi-fi or usb
>* Edit [config.yml](https://raw.githubusercontent.com/vakarianplay/GoMeshtasticBot/refs/heads/main/config.yml) for your configuration
>* Run execute


## 📑 Dependencies

>* goMesh: https://github.com/lmatte7/gomesh
>* yaml: gopkg.in/yaml.v3
>* mautrix: maunium.net/go/mautrix



## 🔧 Build

>* `go build ./messages.go ./cfgload.go ./csvprocessor.go ./relayer.go`

