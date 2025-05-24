package src

import (
	"fmt"
	"mandator-sporades/common"
	"mandator-sporades/proto"
	"strconv"
	"time"
)

// checks if the sender is in the set of messages

func (rp *Replica) senderInMessages(messages []*proto.AsyncConsensus, sender int32) bool {
	for i := 0; i < len(messages); i++ {
		if messages[i].Sender == sender {
			return true
		}
	}
	return false
}

/*
	Handler for sync consensus vote messages
*/

func (rp *Replica) handleConsensusVoteSync(message *proto.AsyncConsensus) {
	// if the rank v,r of the message is greater than or equals to v_cur, r_cur and is Async is false
	if rp.hasGreaterThanOrEqualRank(message.V, message.R, rp.asyncConsensus.vCurr, rp.asyncConsensus.rCurr) {
		if rp.asyncConsensus.isAsync == false {
			//	Save the vote in the vote replies:
			key := strconv.Itoa(int(message.V)) + "." + strconv.Itoa(int(message.R))
			//	if vote replies already has v.r key, then append to existing array, else create a new entry
			_, ok := rp.asyncConsensus.voteReplies[key]
			if !ok {
				rp.asyncConsensus.voteReplies[key] = make([]*proto.AsyncConsensus, 0)
			}

			if rp.senderInMessages(rp.asyncConsensus.voteReplies[key], message.Sender) {
				return
			}

			rp.asyncConsensus.voteReplies[key] = append(rp.asyncConsensus.voteReplies[key], message)
			//	if for this v,r the array vote replies has n-f blocks
			votes, _ := rp.asyncConsensus.voteReplies[key]
			if len(votes) == rp.numReplicas/2+1 {
				//	set block high to be the block with the highest rank in the received set of vote messages and my own block high
				rp.asyncConsensus.blockHigh = rp.extractHighestRankedBlockHigh(votes)
				//	If n-f received vote messages have the same block high and the rank of this received block high is (v,r):
				blockHigh, isSameHigh := rp.hasSameBlockHigh(votes)
				newBlockCommit := rp.asyncConsensus.blockCommit
				if isSameHigh && blockHigh.V == message.V && blockHigh.R == message.R {
					//	set block commit to this block high
					if rp.hasGreaterRank(blockHigh.V, blockHigh.R, rp.asyncConsensus.blockCommit.V, rp.asyncConsensus.blockCommit.R) &&
						rp.hasGreaterRank(blockHigh.V, blockHigh.R, rp.asyncConsensus.lastCommittedBlock.V, rp.asyncConsensus.lastCommittedBlock.R) {
						newBlockCommit = blockHigh
					}
				}
				// 	set v_cur, r_cur to v, r
				rp.asyncConsensus.vCurr = message.V
				rp.asyncConsensus.rCurr = message.R
				//	if I am the leader of Vcur
				if rp.getLeader(rp.asyncConsensus.vCurr) == rp.name {
					//	Form a new block B=(cmnds, v cur , r cur +1 , block high)
					newBlock := proto.AsyncConsensus_Block{
						// creator_name.v.r.type.level. type can be r (regular) or f (fallback), level can be 1,2 or -1 (for regular blocks)
						Id:       strconv.Itoa(int(rp.name)) + "." + strconv.Itoa(int(rp.asyncConsensus.vCurr)) + "." + strconv.Itoa(int(rp.asyncConsensus.rCurr+1)) + "." + "r" + "." + strconv.Itoa(int(-1)),
						V:        rp.asyncConsensus.vCurr,
						R:        rp.asyncConsensus.rCurr + 1,
						Parent:   rp.asyncConsensus.blockHigh,
						Commands: rp.convertToInt32Array(rp.memPool.lastCompletedRounds),
						Level:    -1,
					}

					// add the new block to pool
					rp.asyncConsensus.consensusPool.Add(&newBlock)

					//	broadcast <propose, new block, block commit >
					if rp.debugOn {
						common.Debug("broadcasting propose type 1", 0, rp.debugLevel, rp.debugOn)
					}

					if rp.isAsynchronous {

						epoch := time.Now().Sub(rp.asyncConsensus.startTime).Milliseconds() / int64(rp.timeEpochSize)

						if rp.amIAttacked(int(epoch)) {
							time.Sleep(time.Duration(rp.asynchronousTime) * time.Millisecond)
						}
					}

					for name, _ := range rp.replicaAddrList {

						proposeMsg := proto.AsyncConsensus{
							Sender:      rp.name,
							Receiver:    name,
							UniqueId:    "",
							Type:        1,
							Note:        "",
							V:           rp.asyncConsensus.vCurr,
							R:           rp.asyncConsensus.rCurr + 1,
							BlockHigh:   nil,
							BlockNew:    rp.makeGreatGrandParentNil(&newBlock),
							BlockCommit: rp.makeGreatGrandParentNil(newBlockCommit),
						}

						rpcPair := common.RPCPair{
							Code: rp.messageCodes.AsyncConsensus,
							Obj:  &proposeMsg,
						}

						rp.sendMessage(name, rpcPair)
						if rp.debugOn {
							common.Debug("Sent propose type 1 to "+strconv.Itoa(int(name)), 0, rp.debugLevel, rp.debugOn)
						}
					}
				}

			}
		} else {

			if message.V > rp.asyncConsensus.vCurr { // process later
				rpcPair := common.RPCPair{
					Code: rp.messageCodes.AsyncConsensus,
					Obj:  message,
				}
				rp.sendMessage(rp.name, rpcPair)
				if rp.debugOn {
					common.Debug("Sent an internal sync vote of rank "+fmt.Sprintf("view: %v, round: %v", message.V, message.R)+" because I still haven't changed my mode to sync and my rank is "+fmt.Sprintf("view: %v, round: %v", rp.asyncConsensus.vCurr, rp.asyncConsensus.rCurr)+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.asyncConsensus.startTime)), 0, rp.debugLevel, rp.debugOn)
				}
			}
		}
	} else {
		if rp.debugOn {
			common.Debug("Rejected a sync vote message because its for a previous rank of "+fmt.Sprintf("view: %v, round: %v", message.V, message.R)+" where as I am in "+fmt.Sprintf("view: %v, round: %v", rp.asyncConsensus.vCurr, rp.asyncConsensus.rCurr)+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.asyncConsensus.startTime)), 0, rp.debugLevel, rp.debugOn)
		}
	}
}

/*
	util method to check if all votes contain the same blockHigh
*/

func (rp *Replica) hasSameBlockHigh(votes []*proto.AsyncConsensus) (*proto.AsyncConsensus_Block, bool) {
	blockHigh := votes[0].BlockHigh
	for i := 1; i < len(votes); i++ {
		if blockHigh.Id != votes[i].BlockHigh.Id {
			return nil, false
		}
	}
	return blockHigh, true
}

/*
	Handler for sync consensus propose messages
*/

func (rp *Replica) handleConsensusProposeSync(message *proto.AsyncConsensus) {

	// if the new block has a rank greater v_cur, r_cur

	if rp.hasGreaterRank(message.BlockNew.V, message.BlockNew.R, rp.asyncConsensus.vCurr, rp.asyncConsensus.rCurr) {
		if rp.asyncConsensus.isAsync == false {
			// cancel the timer
			if rp.asyncConsensus.viewTimer != nil {
				rp.asyncConsensus.viewTimer.Cancel()
				rp.asyncConsensus.viewTimer = nil
			}
			//	Save the new block in the consensus pool
			rp.asyncConsensus.consensusPool.Add(message.BlockNew)
			//	update v_cur, r_cur to that of v,r of the block
			rp.asyncConsensus.vCurr = message.BlockNew.V
			rp.asyncConsensus.rCurr = message.BlockNew.R
			// set block high to the new block
			rp.asyncConsensus.blockHigh = message.BlockNew
			// set block commit to block commit, and call updateSMR()
			if rp.hasGreaterRank(message.BlockCommit.V, message.BlockCommit.R, rp.asyncConsensus.blockCommit.V, rp.asyncConsensus.blockCommit.R) &&
				rp.hasGreaterRank(message.BlockCommit.V, message.BlockCommit.R, rp.asyncConsensus.lastCommittedBlock.V, rp.asyncConsensus.lastCommittedBlock.R) {
				rp.asyncConsensus.blockCommit = message.BlockCommit
				rp.asyncConsensus.consensusPool.Add(message.BlockCommit)
				rp.updateSMR()
			}
			// 	send <vote, v cur , r cur , block high > to Vcur leader
			nextLeader := rp.getLeader(rp.asyncConsensus.vCurr)
			if rp.debugOn {
				common.Debug("Sending sync vote to "+strconv.Itoa(int(nextLeader)), 0, rp.debugLevel, rp.debugOn)
			}

			voteMsg := proto.AsyncConsensus{
				Sender:      rp.name,
				Receiver:    nextLeader,
				UniqueId:    "",
				Type:        2,
				Note:        "",
				V:           rp.asyncConsensus.vCurr,
				R:           rp.asyncConsensus.rCurr,
				BlockHigh:   rp.makeGreatGrandParentNil(rp.asyncConsensus.blockHigh),
				BlockNew:    nil,
				BlockCommit: nil,
			}

			rpcPair := common.RPCPair{
				Code: rp.messageCodes.AsyncConsensus,
				Obj:  &voteMsg,
			}

			rp.sendMessage(nextLeader, rpcPair)
			if rp.debugOn {
				common.Debug("Sent sync vote to "+strconv.Itoa(int(nextLeader)), 0, rp.debugLevel, rp.debugOn)
			}

			// start the timeout
			rp.setViewTimer()
		} else {
			if message.V > rp.asyncConsensus.vCurr {
				// process later
				rpcPair := common.RPCPair{
					Code: rp.messageCodes.AsyncConsensus,
					Obj:  message,
				}
				rp.sendMessage(rp.name, rpcPair)
				if rp.debugOn {
					common.Debug("Sent an internal sync propose of rank "+fmt.Sprintf("view: %v, round: %v", message.V, message.R)+" because I still haven't changed my mode to sync and my rank is "+fmt.Sprintf("view: %v, round: %v", rp.asyncConsensus.vCurr, rp.asyncConsensus.rCurr)+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.asyncConsensus.startTime)), 0, rp.debugLevel, rp.debugOn)
				}
			}
		}
	} else {
		if rp.debugOn {
			common.Debug("Rejected a propose sync message because its for a previous rank of "+fmt.Sprintf("view: %v, round: %v", message.V, message.R)+" where as I am in "+fmt.Sprintf("view: %v, round: %v", rp.asyncConsensus.vCurr, rp.asyncConsensus.rCurr)+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.asyncConsensus.startTime)), 0, rp.debugLevel, rp.debugOn)
		}
	}
}

/*
	Upon a timeout, broadcast a timeout message with vcur
*/

func (rp *Replica) handleConsensusInternalTimeout(message *proto.AsyncConsensus) {
	// this is triggered by the timeout
	rp.asyncConsensus.viewTimer = nil
	if rp.hasGreaterThanOrEqualRank(message.V, message.R, rp.asyncConsensus.vCurr, rp.asyncConsensus.rCurr) {
		if rp.asyncConsensus.isAsync == false {

			// broadcast <timeout, v cur , r cur , block high >

			for name, _ := range rp.replicaAddrList {

				timeoutMsg := proto.AsyncConsensus{
					Sender:      rp.name,
					Receiver:    name,
					UniqueId:    "",
					Type:        3,
					Note:        "",
					V:           rp.asyncConsensus.vCurr,
					R:           rp.asyncConsensus.rCurr,
					BlockHigh:   rp.makeGreatGrandParentNil(rp.asyncConsensus.blockHigh),
					BlockNew:    nil,
					BlockCommit: nil,
				}

				rpcPair := common.RPCPair{
					Code: rp.messageCodes.AsyncConsensus,
					Obj:  &timeoutMsg,
				}

				rp.sendMessage(name, rpcPair)
				if rp.debugOn {
					common.Debug("Sent timeout to "+strconv.Itoa(int(name)), 4, rp.debugLevel, rp.debugOn)
				}
			}
		} else {
			if rp.debugOn {
				common.Debug("Rejected an an internal timeout notification because I am already in asycn I am in "+fmt.Sprintf("view: %v, round: %v", rp.asyncConsensus.vCurr, rp.asyncConsensus.rCurr)+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.asyncConsensus.startTime)), 0, rp.debugLevel, rp.debugOn)
			}
		}
	} else {
		if rp.debugOn {
			common.Debug("Rejected an internal timeout message because its for a previous rank of "+fmt.Sprintf("view: %v, round: %v", message.V, message.R)+" where as I am in "+fmt.Sprintf("view: %v, round: %v", rp.asyncConsensus.vCurr, rp.asyncConsensus.rCurr)+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.asyncConsensus.startTime)), 0, rp.debugLevel, rp.debugOn)
		}
	}
}
