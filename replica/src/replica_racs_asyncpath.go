package src

import (
	"fmt"
	"sadl-racs/common"
	"sadl-racs/proto"
	"strconv"
	"strings"
	"time"
)

/*
	handler for external consensus timeout messages
*/

func (rp *Replica) handleConsensusTimeout(message *proto.AsyncConsensus) {
	// if the view of the message is greater than or equals vCur and isAsync is false
	if message.V >= rp.asyncConsensus.vCurr {
		if rp.asyncConsensus.isAsync == false {
			//  save the timeout message in the timeoutMessages
			_, ok := rp.asyncConsensus.timeoutMessages[message.V]
			if !ok {
				rp.asyncConsensus.timeoutMessages[message.V] = make([]*proto.AsyncConsensus, 0)
			}

			if rp.senderInMessages(rp.asyncConsensus.timeoutMessages[message.V], message.Sender) {
				return
			}

			rp.asyncConsensus.timeoutMessages[message.V] = append(rp.asyncConsensus.timeoutMessages[message.V], message)

			timeouts, _ := rp.asyncConsensus.timeoutMessages[message.V]

			//	if n-f timeout messages for the v of message
			if len(timeouts) == rp.numReplicas/2+1 {
				//	set is Async to true
				rp.asyncConsensus.isAsync = true
				if rp.debugOn {
					common.Debug("Entering view change in view "+strconv.Itoa(int(rp.asyncConsensus.vCurr)), 4, rp.debugLevel, rp.debugOn)
				}
				//	Update block high to be the block in the timeout messages with the highest rank and my own block high
				tempBlockHigh := rp.extractHighestRankedBlockHigh(timeouts)
				if rp.hasGreaterRank(tempBlockHigh.V, tempBlockHigh.R, rp.asyncConsensus.blockHigh.V, rp.asyncConsensus.blockHigh.R) {
					rp.asyncConsensus.blockHigh = tempBlockHigh
				}
				//	set vCur, rCur to v, max(r cur , block high .r)
				rp.asyncConsensus.vCurr = message.V
				if rp.asyncConsensus.blockHigh.R > rp.asyncConsensus.rCurr {
					rp.asyncConsensus.rCurr = rp.asyncConsensus.blockHigh.R
				}
				//	Form a new height 1 fallback block B f1 =(cmnds, v cur , r cur +1, block high)
				newLevel1FallBackBlock := proto.AsyncConsensus_Block{
					// creator_name.v.r.type.level. type can be r (regular) or f (fallback), level can be 1,2 or -1 (for regular blocks)
					Id:       strconv.Itoa(int(rp.name)) + "." + strconv.Itoa(int(rp.asyncConsensus.vCurr)) + "." + strconv.Itoa(int(rp.asyncConsensus.rCurr+1)) + "." + "f" + "." + strconv.Itoa(int(1)),
					V:        rp.asyncConsensus.vCurr,
					R:        rp.asyncConsensus.rCurr + 1,
					Parent:   rp.asyncConsensus.blockHigh,
					Commands: rp.convertToInt32Array(rp.memPool.lastCompletedRounds),
					Level:    1,
				}

				// save the new block in the store
				rp.asyncConsensus.consensusPool.Add(&newLevel1FallBackBlock)
				//	broadcast <propose-async, B f1>
				for name, _ := range rp.replicaAddrList {

					proposeAsyncMsg := proto.AsyncConsensus{
						Sender:      rp.name,
						Receiver:    name,
						UniqueId:    "",
						Type:        4,
						Note:        "",
						V:           rp.asyncConsensus.vCurr,
						R:           rp.asyncConsensus.rCurr + 1,
						BlockHigh:   nil,
						BlockNew:    rp.makeGreatGrandParentNil(&newLevel1FallBackBlock),
						BlockCommit: nil,
					}

					rpcPair := common.RPCPair{
						Code: rp.messageCodes.AsyncConsensus,
						Obj:  &proposeAsyncMsg,
					}

					rp.sendMessage(name, rpcPair)
					if rp.debugOn {
						common.Debug("Sent propose-async level 1 to "+strconv.Itoa(int(name)), 2, rp.debugLevel, rp.debugOn)
					}
				}

				rp.asyncConsensus.sentLevel2Block[message.V] = false

			}
		} else {
			// process later
			if message.V > rp.asyncConsensus.vCurr {
				rpcPair := common.RPCPair{
					Code: rp.messageCodes.AsyncConsensus,
					Obj:  message,
				}
				rp.sendMessage(rp.name, rpcPair)
				if rp.debugOn {
					common.Debug("Sent an internal timeout because it is for a future view change ", 2, rp.debugLevel, rp.debugOn)
				}
			}
		}
	} else {
		if rp.debugOn {
			common.Debug("Rejected an external timeout message because I am in a higher view; my rank is "+fmt.Sprintf("view: %v, round: %v", rp.asyncConsensus.vCurr, rp.asyncConsensus.rCurr)+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.asyncConsensus.startTime)), 0, rp.debugLevel, rp.debugOn)
		}
	}
}

/*
	Handler for async propose fallback messages
*/

func (rp *Replica) handleConsensusProposeAsync(message *proto.AsyncConsensus) {
	// if the v of message is equal to v cur
	if message.V == rp.asyncConsensus.vCurr {
		//if is async is true
		if rp.asyncConsensus.isAsync == true {
			if rp.hasGreaterRank(message.BlockNew.V, message.BlockNew.R, rp.asyncConsensus.vCurr, rp.asyncConsensus.rCurr) {
				// save the block in the store
				rp.asyncConsensus.consensusPool.Add(message.BlockNew)
				// send <vote-async, B, h> to p
				voteAsyncMsg := proto.AsyncConsensus{
					Sender:      rp.name,
					Receiver:    message.Sender,
					UniqueId:    message.BlockNew.Id,
					Type:        5,
					Note:        "",
					V:           rp.asyncConsensus.vCurr,
					R:           rp.asyncConsensus.rCurr,
					BlockHigh:   nil,
					BlockNew:    rp.makeGreatGrandParentNil(message.BlockNew),
					BlockCommit: nil,
				}

				rpcPair := common.RPCPair{
					Code: rp.messageCodes.AsyncConsensus,
					Obj:  &voteAsyncMsg,
				}

				rp.sendMessage(message.Sender, rpcPair)
				if rp.debugOn {
					common.Debug("Sent vote-async 5 to "+strconv.Itoa(int(message.Sender)), 2, rp.debugLevel, rp.debugOn)
				}

				// if h == 2 set B fall [p] to B, and adapt level 1 block unless I have already sent level 2 block
				if message.BlockNew.Level == 2 {

					// save the level 2 block in b_fall

					key := strconv.Itoa(int(message.V)) + "." + strconv.Itoa(int(message.BlockNew.Level))
					_, ok := rp.asyncConsensus.bFall[key]
					if !ok {
						rp.asyncConsensus.bFall[key] = make([]string, 0)
					}

					for i := 0; i < len(rp.asyncConsensus.bFall[key]); i++ {
						if rp.asyncConsensus.bFall[key][i] == message.BlockNew.Id {
							return
						}
					}

					rp.asyncConsensus.bFall[key] = append(rp.asyncConsensus.bFall[key], message.BlockNew.Id)

					// if I still haven't sent a level 2 block, adapt the level 1 block, and send a level 2 block

					if rp.asyncConsensus.sentLevel2Block[message.V] == false {
						level1Block := message.BlockNew.Parent
						if level1Block != nil {
							if rp.debugOn {
								common.Debug("Adopted level 1 fallback block from  "+strconv.Itoa(int(message.Sender)), 2, rp.debugLevel, rp.debugOn)
							}
							rp.asyncConsensus.consensusPool.Add(level1Block)
							// create height 2 block and broadcast
							newLevel2FallBackBlock := proto.AsyncConsensus_Block{
								// creator_name.v.r.type.level. type can be r (regular) or f (fallback), level can be 1,2 or -1 (for regular blocks)
								Id:       strconv.Itoa(int(rp.name)) + "." + strconv.Itoa(int(rp.asyncConsensus.vCurr)) + "." + strconv.Itoa(int(level1Block.R+1)) + "." + "f" + "." + strconv.Itoa(int(2)),
								V:        rp.asyncConsensus.vCurr,
								R:        level1Block.R + 1,
								Parent:   level1Block,
								Commands: rp.convertToInt32Array(rp.memPool.lastCompletedRounds),
								Level:    2,
							}

							// save the new block in the store
							rp.asyncConsensus.consensusPool.Add(&newLevel2FallBackBlock)

							//	broadcast <propose-async, B f2>

							for name, _ := range rp.replicaAddrList {

								proposeAsyncLevel2Msg := proto.AsyncConsensus{
									Sender:      rp.name,
									Receiver:    name,
									UniqueId:    "",
									Type:        4,
									Note:        "",
									V:           rp.asyncConsensus.vCurr,
									R:           level1Block.R + 1,
									BlockHigh:   nil,
									BlockNew:    rp.makeGreatGrandParentNil(&newLevel2FallBackBlock),
									BlockCommit: nil,
								}

								rpcPair := common.RPCPair{
									Code: rp.messageCodes.AsyncConsensus,
									Obj:  &proposeAsyncLevel2Msg,
								}

								rp.sendMessage(name, rpcPair)
								if rp.debugOn {
									common.Debug("Sent adopted and extended propose-async level 2 to "+strconv.Itoa(int(name)), 2, rp.debugLevel, rp.debugOn)
								}
							}
							rp.asyncConsensus.sentLevel2Block[message.V] = true
						}
					}

				}
			}
		} else {
			// given that there is level 1 fallback block, eventually i should also get it, so save this incoming message to process later
			rpcPair := common.RPCPair{
				Code: rp.messageCodes.AsyncConsensus,
				Obj:  message,
			}
			rp.sendMessage(rp.name, rpcPair)
			if rp.debugOn {
				common.Debug("Sent an internal fallback propose level 1/2 block because I still haven't received n-f timeouts ", 1, rp.debugLevel, rp.debugOn)
			}
		}
	} else if message.V > rp.asyncConsensus.vCurr {
		rpcPair := common.RPCPair{
			Code: rp.messageCodes.AsyncConsensus,
			Obj:  message,
		}
		rp.sendMessage(rp.name, rpcPair)
		if rp.debugOn {
			common.Debug("Sent an internal propose async level 1/2 block because I still haven't reached the view ", 1, rp.debugLevel, rp.debugOn)
		}
	}
}

/*
handler for consensus async vote messages
*/
func (rp *Replica) handleConsensusAsyncVote(message *proto.AsyncConsensus) {
	//	if the view is equal to current view and  isAsync is true
	if message.BlockNew.V == rp.asyncConsensus.vCurr && rp.asyncConsensus.isAsync == true {
		//	save the vote in the async vote store
		key := message.BlockNew.Id
		_, ok := rp.asyncConsensus.consensusPool.Get(key)
		if !ok {
			panic("Key " + key + " is not found in the block store, which is a fallback block. Triggering this error after receiving an async-vote")
		}

		acks := rp.asyncConsensus.consensusPool.GetAcks(key)
		for i := 0; i < len(acks); i++ {
			if acks[i] == message.Sender {
				return
			}
		}

		rp.asyncConsensus.consensusPool.AddAck(key, message.Sender)

		//	if there are n-f async votes for the block that I proposed
		acks = rp.asyncConsensus.consensusPool.GetAcks(key)
		if len(acks) == rp.numReplicas/2+1 {

			if message.BlockNew.Level == 1 && rp.asyncConsensus.sentLevel2Block[rp.asyncConsensus.vCurr] == false {

				l1block, _ := rp.asyncConsensus.consensusPool.Get(key)
				rLevel1 := l1block.R
				//	Form a new height 2 fallback block B f2 =(cmnds, v cur , B.r+1, B, 2)
				newLevel2FallBackBlock := proto.AsyncConsensus_Block{
					Id:       strconv.Itoa(int(rp.name)) + "." + strconv.Itoa(int(rp.asyncConsensus.vCurr)) + "." + strconv.Itoa(int(rLevel1+1)) + "." + "f" + "." + strconv.Itoa(int(2)), //creator_name.v.r.type.level. type can be r (regular) or f (fallback), level can be 1,2 or -1 (for regular blocks)
					V:        rp.asyncConsensus.vCurr,
					R:        rLevel1 + 1,
					Parent:   l1block,
					Commands: rp.convertToInt32Array(rp.memPool.lastCompletedRounds),
					Level:    2,
				}

				rp.asyncConsensus.consensusPool.Add(&newLevel2FallBackBlock)

				//	broadcast <propose-async, B f2 , self.id, 2>
				for name, _ := range rp.replicaAddrList {

					proposeAsyncLevel2Msg := proto.AsyncConsensus{
						Sender:      rp.name,
						Receiver:    name,
						UniqueId:    "",
						Type:        4,
						Note:        "",
						V:           rp.asyncConsensus.vCurr,
						R:           rLevel1 + 1,
						BlockHigh:   nil,
						BlockNew:    rp.makeGreatGrandParentNil(&newLevel2FallBackBlock),
						BlockCommit: nil,
					}

					rpcPair := common.RPCPair{
						Code: rp.messageCodes.AsyncConsensus,
						Obj:  &proposeAsyncLevel2Msg,
					}

					rp.sendMessage(name, rpcPair)
					if rp.debugOn {
						common.Debug("Sent propose-async level 2 to "+strconv.Itoa(int(name)), 1, rp.debugLevel, rp.debugOn)
					}
				}

				rp.asyncConsensus.sentLevel2Block[rp.asyncConsensus.vCurr] = true

			} else if message.BlockNew.Level == 2 {
				// broadcast <fallback-complete, B, v cur , self.id>
				for name, _ := range rp.replicaAddrList {

					proposeAsyncFallbackComplete := proto.AsyncConsensus{
						Sender:      rp.name,
						Receiver:    name,
						UniqueId:    "",
						Type:        9,
						Note:        "",
						V:           rp.asyncConsensus.vCurr,
						R:           message.BlockNew.R,
						BlockHigh:   nil,
						BlockNew:    rp.makeGreatGrandParentNil(message.BlockNew),
						BlockCommit: nil,
					}

					rpcPair := common.RPCPair{
						Code: rp.messageCodes.AsyncConsensus,
						Obj:  &proposeAsyncFallbackComplete,
					}

					rp.sendMessage(name, rpcPair)
					if rp.debugOn {
						common.Debug("Sent propose-async-fallback-complete to "+strconv.Itoa(int(name)), 1, rp.debugLevel, rp.debugOn)
					}
				}
			}
		}
	}
}

/*
	Handler for async consensus fallback complete messages
*/

func (rp *Replica) handleConsensusFallbackCompleteMessage(message *proto.AsyncConsensus) {
	if message.BlockNew.V == rp.asyncConsensus.vCurr {
		if rp.asyncConsensus.isAsync == true {
			// save the level 2 confirmed block
			rp.asyncConsensus.consensusPool.Add(message.BlockNew)

			// add the id of the confirmed level 2 block to Bfall[v.3]
			key := strconv.Itoa(int(message.BlockNew.V)) + "." + strconv.Itoa(3)

			_, ok := rp.asyncConsensus.bFall[key]

			if !ok {
				rp.asyncConsensus.bFall[key] = make([]string, 0)
			}

			for i := 0; i < len(rp.asyncConsensus.bFall[key]); i++ {
				if rp.asyncConsensus.bFall[key][i] == message.BlockNew.Id {
					return
				}
			}

			rp.asyncConsensus.bFall[key] = append(rp.asyncConsensus.bFall[key], message.BlockNew.Id)

			if len(rp.asyncConsensus.bFall[key]) == rp.numReplicas/2+1 {
				// received majority fallback complete messages

				l := rp.asyncConsensus.randomness[rp.asyncConsensus.vCurr] // l is the index of the leader
				leaderNode := l

				if rp.debugOn {
					common.Debug("Async leader node for the view "+strconv.Itoa(int(rp.asyncConsensus.vCurr))+" is "+strconv.Itoa(leaderNode), 2, rp.debugLevel, rp.debugOn)
				}

				//– if height 2 block by leader exists in the first n-f height 2 blocks received then
				height2ConfirmedLeaderBlockExists := false
				var height2ConfirmedLeaderBlock *proto.AsyncConsensus_Block
				height2ConfirmedLeaderBlock = nil
				for j := 0; j < len(rp.asyncConsensus.bFall[key]); j++ {
					creator := strings.Split(rp.asyncConsensus.bFall[key][j], ".")[0]
					if creator == strconv.Itoa(leaderNode) {
						height2ConfirmedLeaderBlockExists = true
						height2ConfirmedLeaderBlock, _ = rp.asyncConsensus.consensusPool.Get(rp.asyncConsensus.bFall[key][j])
						break
					}
				}
				if height2ConfirmedLeaderBlockExists {
					//	Set block high, block commit to height 2 block from l
					rp.asyncConsensus.blockHigh = height2ConfirmedLeaderBlock

					if rp.hasGreaterRank(height2ConfirmedLeaderBlock.V, height2ConfirmedLeaderBlock.R, rp.asyncConsensus.blockCommit.V, rp.asyncConsensus.blockCommit.R) &&
						rp.hasGreaterRank(height2ConfirmedLeaderBlock.V, height2ConfirmedLeaderBlock.R, rp.asyncConsensus.lastCommittedBlock.V, rp.asyncConsensus.lastCommittedBlock.R) {
						rp.asyncConsensus.blockCommit = height2ConfirmedLeaderBlock
						rp.updateSMR()
					}

					if rp.debugOn {
						common.Debug("Updated block commit in the async path for block "+rp.asyncConsensus.blockCommit.Id, 2, rp.debugLevel, rp.debugOn)
					}
					//	Set v cur , r cur to rank(block high)
					rp.asyncConsensus.vCurr = message.V
					rp.asyncConsensus.rCurr = rp.asyncConsensus.blockHigh.R
				} else {
					if rp.debugOn {
						common.Debug("Leader node level 2 confirmed proposal does not exist for the view "+strconv.Itoa(int(rp.asyncConsensus.vCurr)), 2, rp.debugLevel, rp.debugOn)
					}
					//else if height 2 block from the leader exists in the Bfall
					height2Key := strconv.Itoa(int(message.V)) + "." + strconv.Itoa(2)
					height2Blocks, ok := rp.asyncConsensus.bFall[height2Key]
					if ok {
						height2LeaderBlockExists := false
						var height2LeaderBlock *proto.AsyncConsensus_Block
						height2LeaderBlock = nil
						for k := 0; k < len(height2Blocks); k++ {
							creator := strings.Split(height2Blocks[k], ".")[0]
							if creator == strconv.Itoa(leaderNode) {
								height2LeaderBlockExists = true
								height2LeaderBlock, _ = rp.asyncConsensus.consensusPool.Get(height2Blocks[k])
								break
							}
						}
						if height2LeaderBlockExists {
							//	Set block high to height2LeaderBlock
							rp.asyncConsensus.blockHigh = height2LeaderBlock
							//	Set v cur , r cur to rank(block high)
							rp.asyncConsensus.vCurr = message.V
							rp.asyncConsensus.rCurr = rp.asyncConsensus.blockHigh.R
							if rp.debugOn {
								common.Debug("Updated block high (not committed) in the async path for block "+rp.asyncConsensus.blockHigh.Id, 2, rp.debugLevel, rp.debugOn)
							}
						} else {
							if rp.debugOn {
								common.Debug("Leader node level 2 proposal does not exists for the view "+strconv.Itoa(int(rp.asyncConsensus.vCurr)), 2, rp.debugLevel, rp.debugOn)
							}
						}
					}
				}
				//	Set v cur to v cur +1
				rp.asyncConsensus.vCurr++
				if rp.debugOn {
					common.Debug("Incremented the view to "+strconv.Itoa(int(rp.asyncConsensus.vCurr)), 2, rp.debugLevel, rp.debugOn)
				}
				//	Set isAsync to false
				rp.asyncConsensus.isAsync = false
				// send <vote, v cur , r cur , block high > to L_Vcur

				nextLeader := rp.getLeader(rp.asyncConsensus.vCurr)

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
					common.Debug("Exiting view change and sending vote to "+strconv.Itoa(int(nextLeader))+" after the view change", 4, rp.debugLevel, rp.debugOn)
				}

				// start the timeout
				rp.setViewTimer()
			}
		} else {
			rpcPair := common.RPCPair{
				Code: rp.messageCodes.AsyncConsensus,
				Obj:  message,
			}
			rp.sendMessage(rp.name, rpcPair)
			if rp.debugOn {
				common.Debug("Sent an internal fallback-complete because I still haven't converted to async ", 1, rp.debugLevel, rp.debugOn)
			}
		}
	} else if message.BlockNew.V > rp.asyncConsensus.vCurr {
		rpcPair := common.RPCPair{
			Code: rp.messageCodes.AsyncConsensus,
			Obj:  message,
		}

		rp.sendMessage(rp.name, rpcPair)

		if rp.debugOn {
			common.Debug("Sent an internal fallback-complete because I still haven't reached the view ", 1, rp.debugLevel, rp.debugOn)
		}
	}
}
