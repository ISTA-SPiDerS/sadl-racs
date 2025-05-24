package src

import (
	"fmt"
	"mandator-sporades/common"
	"mandator-sporades/proto"
	"strconv"
	"time"
)

/*
	Upon a view change / upon bootstrap send a prepare message for all instances from last decided index +1 to len(log)
*/

func (rp *Replica) sendPrepare() {

	rp.createInstanceIfMissing(int(rp.paxosConsensus.lastDecidedLogIndex + 1))

	// reset the promise response map, because all we care is new view change messages
	rp.paxosConsensus.promiseResponses = make(map[int32][]*proto.PaxosConsensus)

	if rp.paxosConsensus.lastPromisedBallot > rp.paxosConsensus.lastPreparedBallot {
		rp.paxosConsensus.lastPreparedBallot = rp.paxosConsensus.lastPromisedBallot
	}
	rp.paxosConsensus.lastPreparedBallot = rp.paxosConsensus.lastPreparedBallot + 100*rp.name + 2

	rp.paxosConsensus.state = "C" // become a contestant
	// increase the view number
	rp.paxosConsensus.view++
	// broadcast a prepare message
	for name, _ := range rp.replicaAddrList {
		prepareMsg := proto.PaxosConsensus{
			Sender:         rp.name,
			Receiver:       name,
			UniqueId:       "",
			Type:           1,
			Note:           "",
			InstanceNumber: rp.paxosConsensus.lastDecidedLogIndex + 1,
			PrepareBallot:  rp.paxosConsensus.lastPreparedBallot,
			PromiseBallot:  -1,
			ProposeBallot:  -1,
			AcceptBalllot:  -1,
			View:           rp.paxosConsensus.view,
			PromiseReply:   nil,
			ProposeValue:   nil,
			DecidedValue:   nil,
		}

		rpcPair := common.RPCPair{
			Code: rp.messageCodes.PaxosConsensus,
			Obj:  &prepareMsg,
		}

		rp.sendMessage(name, rpcPair)
		if rp.debugOn {
			common.Debug("Sent prepare to "+strconv.Itoa(int(name)), 1, rp.debugLevel, rp.debugOn)
		}
	}

	// cancel the view timer
	if rp.paxosConsensus.viewTimer != nil {
		rp.paxosConsensus.viewTimer.Cancel()
		rp.paxosConsensus.viewTimer = nil
	}
	// set the view timer
	rp.setPaxosViewTimer(rp.paxosConsensus.view, rp.paxosConsensus.lastDecidedLogIndex)

	// cancel the current leader
	rp.paxosConsensus.currentLeader = -1

}

/*
	Handler for prepare message, check if it is possible to promise for all instances from initial index to len(log)-1, if yes send a response
	if at least one instance does not agree with the prepare ballot, do not send anything
*/

func (rp *Replica) handlePrepare(message *proto.PaxosConsensus) {
	prepared := true

	// the view of prepare should be from a higher view
	if rp.paxosConsensus.view < message.View || (rp.paxosConsensus.view == message.View && message.Sender == rp.name) {

		prepareResponses := make([]*proto.PaxosConsensusInstance, 0)

		for i := message.InstanceNumber; i < int32(len(rp.paxosConsensus.replicatedLog)); i++ {

			prepareResponses = append(prepareResponses, &proto.PaxosConsensusInstance{
				InstanceNumber:     i,
				LastAcceptedBallot: rp.paxosConsensus.replicatedLog[i].acceptedBallot,
				LastAcceptedValue:  rp.paxosConsensus.replicatedLog[i].acceptedValues,
			})

			if rp.paxosConsensus.replicatedLog[i].promisedBallot >= message.PrepareBallot {
				prepared = false
				break
			}
		}

		if prepared == true {

			// cancel the view timer
			if rp.paxosConsensus.viewTimer != nil {
				rp.paxosConsensus.viewTimer.Cancel()
				rp.paxosConsensus.viewTimer = nil
			}

			rp.paxosConsensus.lastPromisedBallot = message.PrepareBallot

			if message.Sender != rp.name {
				// become follower
				rp.paxosConsensus.state = "A"
				rp.paxosConsensus.currentLeader = message.Sender
				rp.paxosConsensus.view = message.View
			}

			for i := message.InstanceNumber; i < int32(len(rp.paxosConsensus.replicatedLog)); i++ {
				rp.paxosConsensus.replicatedLog[i].promisedBallot = message.PrepareBallot
			}

			// send a promise message to the sender
			promiseMsg := proto.PaxosConsensus{
				Sender:         rp.name,
				Receiver:       message.Sender,
				UniqueId:       "",
				Type:           2,
				Note:           "",
				InstanceNumber: message.InstanceNumber,
				PrepareBallot:  -1,
				PromiseBallot:  message.PrepareBallot,
				ProposeBallot:  -1,
				AcceptBalllot:  -1,
				View:           message.View,
				PromiseReply:   prepareResponses,
				ProposeValue:   nil,
				DecidedValue:   nil,
			}

			rpcPair := common.RPCPair{
				Code: rp.messageCodes.PaxosConsensus,
				Obj:  &promiseMsg,
			}

			rp.sendMessage(message.Sender, rpcPair)
			if rp.debugOn {
				common.Debug("Sent promise to "+strconv.Itoa(int(message.Sender)), 1, rp.debugLevel, rp.debugOn)
			}

			// set the view timer
			rp.setPaxosViewTimer(rp.paxosConsensus.view, rp.paxosConsensus.lastDecidedLogIndex)
		}
	}

}

/*
	Handler for promise messages
*/

func (rp *Replica) handlePromise(message *proto.PaxosConsensus) {
	if message.PromiseBallot == rp.paxosConsensus.lastPreparedBallot && message.View == rp.paxosConsensus.view && rp.paxosConsensus.state == "C" {
		// save the promise message
		_, ok := rp.paxosConsensus.promiseResponses[message.View]
		if !ok {
			rp.paxosConsensus.promiseResponses[message.View] = make([]*proto.PaxosConsensus, 0)
		}
		rp.paxosConsensus.promiseResponses[message.View] = append(rp.paxosConsensus.promiseResponses[message.View], message)

		if len(rp.paxosConsensus.promiseResponses[message.View]) == rp.numReplicas/2+1 {
			// we have majority promise messages for the same view,
			// update the highest accepted ballot and the values
			for i := 0; i < len(rp.paxosConsensus.promiseResponses[message.View]); i++ {
				lastAcceptedEntries := rp.paxosConsensus.promiseResponses[message.View][i].PromiseReply
				for j := 0; j < len(lastAcceptedEntries); j++ {
					instanceNumber := lastAcceptedEntries[j].InstanceNumber
					rp.createInstanceIfMissing(int(instanceNumber))
					if lastAcceptedEntries[j].LastAcceptedBallot > rp.paxosConsensus.replicatedLog[instanceNumber].highestSeenAcceptedBallot {
						rp.paxosConsensus.replicatedLog[instanceNumber].highestSeenAcceptedBallot = lastAcceptedEntries[j].LastAcceptedBallot
						rp.paxosConsensus.replicatedLog[instanceNumber].highestSeenAcceptedValue = lastAcceptedEntries[j].LastAcceptedValue
					}
				}
			}
			rp.paxosConsensus.state = "L"
			if rp.debugOn {
				common.Debug("Became the leader in "+strconv.Itoa(int(rp.paxosConsensus.view))+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.paxosConsensus.startTime)), 4, rp.debugLevel, rp.debugOn)
			}
			rp.paxosConsensus.currentLeader = rp.name
			rp.sendPropose(rp.paxosConsensus.lastDecidedLogIndex + 1)
		}
	}

}

/*
	Leader invokes this function to replicate a new instance for last decided index +1
*/

func (rp *Replica) sendPropose(instance int32) {
	if rp.paxosConsensus.state == "L" && rp.paxosConsensus.lastPreparedBallot >= rp.paxosConsensus.lastPromisedBallot {
		rp.createInstanceIfMissing(int(instance))
		proposeValue := rp.convertToInt32Array(rp.memPool.lastCompletedRounds)
		if rp.paxosConsensus.replicatedLog[instance].highestSeenAcceptedBallot != -1 {
			proposeValue = rp.paxosConsensus.replicatedLog[instance].highestSeenAcceptedValue
		}

		// set the proposed ballot for this instance
		rp.paxosConsensus.replicatedLog[instance].proposedBallot = rp.paxosConsensus.lastPreparedBallot
		rp.paxosConsensus.replicatedLog[instance].proposeResponses = make([]*proto.PaxosConsensus, 0)

		if instance > 1 && (rp.paxosConsensus.replicatedLog[instance-1].decisions == nil || len(rp.paxosConsensus.replicatedLog[instance-1].decisions) < rp.numReplicas) {
			panic("proposing when the last decided index does not have correct decisions")
		}

		if rp.isAsynchronous {

			epoch := time.Now().Sub(rp.paxosConsensus.startTime).Milliseconds() / int64(rp.timeEpochSize)

			if rp.amIAttacked(int(epoch)) {
				time.Sleep(time.Duration(rp.asynchronousTime) * time.Millisecond)
			}
		}

		// send a propose message
		for name, _ := range rp.replicaAddrList {
			proposeMsg := proto.PaxosConsensus{
				Sender:         rp.name,
				Receiver:       name,
				UniqueId:       "",
				Type:           3,
				Note:           "",
				InstanceNumber: instance,
				PrepareBallot:  -1,
				PromiseBallot:  -1,
				ProposeBallot:  rp.paxosConsensus.lastPreparedBallot,
				AcceptBalllot:  -1,
				View:           rp.paxosConsensus.view,
				PromiseReply:   nil,
				ProposeValue:   proposeValue,
				DecidedValue:   rp.paxosConsensus.replicatedLog[instance-1].decisions,
			}

			rpcPair := common.RPCPair{
				Code: rp.messageCodes.PaxosConsensus,
				Obj:  &proposeMsg,
			}

			rp.sendMessage(name, rpcPair)
			if rp.debugOn {
				common.Debug("Sent propose to "+strconv.Itoa(int(name)), 1, rp.debugLevel, rp.debugOn)
			}
		}
	}
}

/*
	Handler for propose message, If the propose ballot number is greater than or equal to the promised ballot number, set the accepted ballot and accepted values, and send
	an accept message, also record the decided message for the previous instance
*/

func (rp *Replica) handlePropose(message *proto.PaxosConsensus) {
	rp.createInstanceIfMissing(int(message.InstanceNumber))

	// if the message is from a future view, become an acceptor and set the new leader
	if message.View > rp.paxosConsensus.view {
		rp.paxosConsensus.view = message.View
		rp.paxosConsensus.currentLeader = message.Sender
		rp.paxosConsensus.state = "A"
	}

	// if this message is for the current view
	if message.Sender == rp.paxosConsensus.currentLeader && message.View == rp.paxosConsensus.view && message.ProposeBallot >= rp.paxosConsensus.replicatedLog[message.InstanceNumber].promisedBallot {

		// cancel the view timer
		if rp.paxosConsensus.viewTimer != nil {
			rp.paxosConsensus.viewTimer.Cancel()
			rp.paxosConsensus.viewTimer = nil
		}

		rp.paxosConsensus.replicatedLog[message.InstanceNumber].acceptedBallot = message.ProposeBallot
		rp.paxosConsensus.replicatedLog[message.InstanceNumber].acceptedValues = message.ProposeValue

		if message.InstanceNumber > 1 {
			// mark the previous instance as decided
			if rp.paxosConsensus.replicatedLog[message.InstanceNumber-1].decided == false {
				if rp.debugOn {
					common.Debug("Decided instance "+strconv.Itoa(int(message.InstanceNumber-1)), 1, rp.debugLevel, rp.debugOn)
				}
				rp.paxosConsensus.replicatedLog[message.InstanceNumber-1].decided = true
				if message.DecidedValue == nil || len(message.DecidedValue) < rp.numReplicas {
					panic("Error in the last decided values for instance " + strconv.Itoa(int(message.InstanceNumber-1)) + " " + fmt.Sprintf(" %v ", message))
				}
				rp.paxosConsensus.replicatedLog[message.InstanceNumber-1].decisions = message.DecidedValue
				rp.updateLastDecidedIndex()
			}
		}
		// send an accept message to the sender
		acceptMsg := proto.PaxosConsensus{
			Sender:         rp.name,
			Receiver:       message.Sender,
			UniqueId:       "",
			Type:           4,
			Note:           "",
			InstanceNumber: message.InstanceNumber,
			PrepareBallot:  -1,
			PromiseBallot:  -1,
			ProposeBallot:  -1,
			AcceptBalllot:  message.ProposeBallot,
			View:           rp.paxosConsensus.view,
			PromiseReply:   nil,
			ProposeValue:   message.ProposeValue,
			DecidedValue:   nil,
		}

		rpcPair := common.RPCPair{
			Code: rp.messageCodes.PaxosConsensus,
			Obj:  &acceptMsg,
		}

		rp.sendMessage(message.Sender, rpcPair)
		if rp.debugOn {
			common.Debug("Sent accept message to "+strconv.Itoa(int(message.Sender)), 1, rp.debugLevel, rp.debugOn)
		}

		// set the view timer
		rp.setPaxosViewTimer(rp.paxosConsensus.view, rp.paxosConsensus.lastDecidedLogIndex)
	}
}

/*
	handler for accept messages. Upon collecting n-f accept messages, mark the instance as decided, call SMR and
*/

func (rp *Replica) handleAccept(message *proto.PaxosConsensus) {
	if int32(len(rp.paxosConsensus.replicatedLog)) < message.InstanceNumber+1 {
		panic("Received accept without having an instance")
	}

	if message.View == rp.paxosConsensus.view && message.InstanceNumber == rp.paxosConsensus.lastDecidedLogIndex+1 && message.AcceptBalllot == rp.paxosConsensus.replicatedLog[message.InstanceNumber].proposedBallot && rp.paxosConsensus.state == "L" {

		// add the accept to the instance
		rp.paxosConsensus.replicatedLog[message.InstanceNumber].proposeResponses = append(rp.paxosConsensus.replicatedLog[message.InstanceNumber].proposeResponses, message)

		// if there are n-f accept messages
		if len(rp.paxosConsensus.replicatedLog[message.InstanceNumber].proposeResponses) == rp.numReplicas/2+1 && rp.paxosConsensus.replicatedLog[message.InstanceNumber].decided == false {
			rp.paxosConsensus.replicatedLog[message.InstanceNumber].decided = true
			rp.paxosConsensus.replicatedLog[message.InstanceNumber].decisions = message.ProposeValue
			if rp.paxosConsensus.replicatedLog[message.InstanceNumber].decisions == nil || len(rp.paxosConsensus.replicatedLog[message.InstanceNumber].decisions) < rp.numReplicas {
				panic("Received n-f accepts but the decision is empty, the last accept reply is " + fmt.Sprintf(" %f ", message))
			}
			rp.updateLastDecidedIndex()
			if rp.debugOn {
				common.Debug("Decided upon receiving n-f accept message for instance "+strconv.Itoa(int(message.InstanceNumber)), 1, rp.debugLevel, rp.debugOn)
			}
			rp.sendPropose(rp.paxosConsensus.lastDecidedLogIndex + 1)
		}
	}
}

/*
	handler for internal timeout messages, send a prepare message
*/

func (rp *Replica) handlePaxosInternalTimeout(message *proto.PaxosConsensus) {
	if rp.debugOn {
		common.Debug("Received a timeout for view "+strconv.Itoa(int(message.View))+" for the last decided index "+strconv.Itoa(int(message.InstanceNumber))+" while my view is "+strconv.Itoa(int(rp.paxosConsensus.view))+" and my last decided index is "+strconv.Itoa(int(rp.paxosConsensus.lastDecidedLogIndex))+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.paxosConsensus.startTime)), 4, rp.debugLevel, rp.debugOn)
	}
	// check if the view timeout is still valid
	if rp.paxosConsensus.view == message.View && rp.paxosConsensus.lastDecidedLogIndex == message.InstanceNumber {
		if rp.debugOn {
			common.Debug("Accepted a timeout for view "+strconv.Itoa(int(message.View))+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.paxosConsensus.startTime)), 4, rp.debugLevel, rp.debugOn)
		}
		rp.sendPrepare()
	}
}

/*
	update the last decided index and call updateSMR method
*/

func (rp *Replica) updateLastDecidedIndex() {
	for i := rp.paxosConsensus.lastDecidedLogIndex + 1; i < int32(len(rp.paxosConsensus.replicatedLog)); i++ {
		if rp.paxosConsensus.replicatedLog[i].decided == true {
			rp.paxosConsensus.lastDecidedLogIndex++
			if rp.debugOn {
				common.Debug("Updated last decided index "+strconv.Itoa(int(rp.paxosConsensus.lastDecidedLogIndex)), 1, rp.debugLevel, rp.debugOn)
			}
			if len(rp.paxosConsensus.replicatedLog[rp.paxosConsensus.lastDecidedLogIndex].decisions) < rp.numReplicas {
				panic("Empty decision array in " + fmt.Sprintf("%v", rp.paxosConsensus.replicatedLog[rp.paxosConsensus.lastDecidedLogIndex]))
			}
		} else {
			break
		}
	}
	rp.updatePaxosSMR()
}
