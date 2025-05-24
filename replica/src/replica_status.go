package src

import (
	"fmt"
	"sadl-racs/common"
	"sadl-racs/proto"
	"time"
)

/*
	Handler for status message
		1. Invoke bootstrap or printlog depending on the operation type
		2. Send a response back to the sender
*/

func (rp *Replica) handleStatus(message *proto.Status) {
	fmt.Print("Status  " + fmt.Sprintf("%v", message) + " \n")
	if message.Type == 1 {
		if rp.serverStarted == false {
			rp.serverStarted = true
			rp.ConnectBootStrap()
			time.Sleep(2 * time.Second)
		}
	} else if message.Type == 2 {
		if rp.logPrinted == false {
			rp.logPrinted = true

			// empty the incoming channel
			go func() {
				for true {
					_ = <-rp.incomingChan
				}
			}()

			//rp.printLogMemPool() // this is for the mem pool testing purposes

			if rp.consAlgo == "async" {
				rp.printLogConsensus() // this is for consensus testing purposes
			}

		}
	} else if message.Type == 3 {
		if rp.consensusStarted == false {
			rp.consensusStarted = true
			if rp.consAlgo == "async" {
				rp.sendGenesisConsensusVote()
			}
		}
	}

	if rp.debugOn {
		common.Debug("Sending status reply ", 0, rp.debugLevel, rp.debugOn)
	}

	statusMessage := proto.Status{
		Type: message.Type,
		Note: message.Note,
	}

	rpcPair := common.RPCPair{
		Code: rp.messageCodes.StatusRPC,
		Obj:  &statusMessage,
	}

	rp.sendMessage(int32(message.Sender), rpcPair)
	if rp.debugOn {
		common.Debug("Sent status ", 0, rp.debugLevel, rp.debugOn)
	}

}
