package main

import (
	"fmt"
	"log"

	"github.com/lmatte7/gomesh"
	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
)

const nodeAddress = "192.168.88.49"

func main() {
	var radio gomesh.Radio

	if err := radio.Init(nodeAddress); err != nil {
		log.Fatal(err)
	}
	defer radio.Close()

	nodeInfo(radio)

}

func nodeInfo(radio gomesh.Radio) {
	responses, err := radio.GetRadioInfo()
	if err != nil {
		log.Fatal(err)
	}

	var myNum uint32
	var myInfo *pb.FromRadio_MyInfo
	var myNode *pb.NodeInfo

	for _, r := range responses {
		if info, ok := r.GetPayloadVariant().(*pb.FromRadio_MyInfo); ok {
			myInfo = info
			myNum = info.MyInfo.MyNodeNum
		}
		if ni, ok := r.GetPayloadVariant().(*pb.FromRadio_NodeInfo); ok {
			if myNum != 0 && ni.NodeInfo.Num == myNum {
				myNode = ni.NodeInfo
			}
		}
	}

	var metrics string
	var name string
	var userId string
	if myNode != nil && myNode.User != nil && myNode.User.LongName != "" {
		name = myNode.User.LongName
		userId = myNode.User.Id
		metrics = myNode.DeviceMetrics.String()
	} else if myNode != nil && myNode.User != nil {
		name = myNode.User.ShortName

	}

	fmt.Println("Node num: ", myNum)
	fmt.Println("ID: ", userId)
	fmt.Println("Name: ", name)
	fmt.Println("Metrics: ", metrics)
	fmt.Println("Node info: ", myInfo.MyInfo.String())
}
