package src

import (
	"fmt"
	"sadl-racs/common"
	"sadl-racs/proto"
	"strconv"
	"time"
)

/*
	when a status response is received, print it to console
*/

func (cl *Client) handleClientStatusResponse(response *proto.Status) {
	fmt.Printf("status response %v\n", response)
}

/*
	Send a status request to all the replicas
*/

func (cl *Client) SendStatus(operationType int) {
	if cl.debugOn {
		cl.debug("Sending status request to all replicas", 0)
	}

	for name, _ := range cl.replicaAddrList {

		statusRequest := proto.Status{
			Type:   int32(operationType),
			Note:   "",
			Sender: int64(cl.clientName),
		}

		rpcPair := common.RPCPair{
			Code: cl.messageCodes.StatusRPC,
			Obj:  &statusRequest,
		}

		cl.sendMessage(name, rpcPair)

		if cl.debugOn {
			cl.debug("Sent status to "+strconv.Itoa(int(name)), 0)
		}
	}
	time.Sleep(time.Duration(statusTimeout) * time.Second)
}
