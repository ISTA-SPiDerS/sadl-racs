package src

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"mandator-sporades/common"
	"mandator-sporades/proto"
	"net"
	"strconv"
	"time"
)

/*
	Given an int32 id, connect to that node
	nodeType should be one of client and replica
*/

func (rp *Replica) ConnectToNode(id int32, address string, nodeType string) {
	if rp.debugOn {
		rp.debug("Connecting to "+strconv.Itoa(int(id)), 0)
	}
	var b [4]byte
	bs := b[:4]

	for true {
		conn, err := net.Dial("tcp", address)
		if err == nil {

			if nodeType == "client" {
				rp.outgoingClientWriters[id] = bufio.NewWriter(conn)
				binary.LittleEndian.PutUint16(bs, uint16(rp.name))
				_, err := conn.Write(bs)
				if err != nil {
					panic("Error while connecting to client " + err.Error())
				}
			} else if nodeType == "replica" {
				rp.outgoingReplicaWriters[id] = bufio.NewWriter(conn)
				binary.LittleEndian.PutUint16(bs, uint16(rp.name))
				_, err := conn.Write(bs)
				if err != nil {
					panic("Error while connecting to replica " + err.Error())
				}
			} else {
				panic("Unknown node id")
			}
			if rp.debugOn {
				rp.debug("Established outgoing connection to "+strconv.Itoa(int(id)), 0)
			}
			break
		}
	}
}

/*
	Connect to all replicas on bootstrap
*/

func (rp *Replica) ConnectBootStrap() {

	for name, address := range rp.replicaAddrList {
		rp.ConnectToNode(name, address, "replica")
	}
}

/*
	listen to a given connection reader. Upon receiving any message, put it into the central incoming buffer
*/

func (rp *Replica) connectionListener(reader *bufio.Reader, id int32) {

	var msgType uint8
	var err error = nil

	for true {

		if msgType, err = reader.ReadByte(); err != nil {
			if rp.debugOn {
				rp.debug("Error while reading message code: connection broken from "+strconv.Itoa(int(id))+" "+err.Error(), 0)
			}
			return
		}

		if rpair, present := rp.rpcTable[msgType]; present {
			obj := rpair.Obj.New()
			if err = obj.Unmarshal(reader); err != nil {
				if rp.debugOn {
					rp.debug("Error while unmarshalling from "+strconv.Itoa(int(id))+err.Error(), 0)
				}
				return
			}
			rp.incomingChan <- &common.RPCPair{
				Code: msgType,
				Obj:  obj,
			}
			if rp.debugOn {
				rp.debug("Pushed a message from "+strconv.Itoa(int(id)), 0)
			}
		} else {
			if rp.debugOn {
				rp.debug("Error received unknown message type from "+strconv.Itoa(int(id)), 0)
			}
			return
		}
	}
}

/*
	listen on the replica port for new connections from clients, and all replicas
*/

func (rp *Replica) WaitForConnections() {
	go func() {
		var b [4]byte
		bs := b[:4]
		Listener, err := net.Listen("tcp", rp.listenAddress)
		if err != nil {
			panic(err.Error())
		}
		if rp.debugOn {
			common.Debug("Listening to incoming connection in "+rp.listenAddress, 0, rp.debugLevel, rp.debugOn)
		}

		for true {
			conn, err := Listener.Accept()
			if err != nil {
				panic("Socket accept error " + err.Error())
			}
			if _, err := io.ReadFull(conn, bs); err != nil {
				panic("Connection read error when establishing incoming connections " + err.Error())
			}
			id := int32(binary.LittleEndian.Uint16(bs))
			if rp.debugOn {
				common.Debug("Received incoming connection from "+strconv.Itoa(int(id)), 0, rp.debugLevel, rp.debugOn)
			}
			nodeType := rp.getNodeType(id)
			if nodeType == "client" {
				rp.incomingClientReaders[id] = bufio.NewReader(conn)
				go rp.connectionListener(rp.incomingClientReaders[id], id)
				if rp.debugOn {
					common.Debug("Started listening to client "+strconv.Itoa(int(id)), 0, rp.debugLevel, rp.debugOn)
				}
				rp.ConnectToNode(id, rp.clientAddrList[id], "client")

			} else if nodeType == "replica" {
				rp.incomingReplicaReaders[id] = bufio.NewReader(conn)
				go rp.connectionListener(rp.incomingReplicaReaders[id], id)
				if rp.debugOn {
					common.Debug("Started listening to replica "+strconv.Itoa(int(id)), 0, rp.debugLevel, rp.debugOn)
				}
			}
		}
	}()
}

/*
	this is the main execution thread that listens to all the incoming messages
	It listens to incoming messages from the incomingChan, and invokes the appropriate handler depending on the message type
*/

func (rp *Replica) Run() {

	for true {

		if rp.debugOn {
			common.Debug("Checking channel..", 0, rp.debugLevel, rp.debugOn)
		}
		replicaMessage := <-rp.incomingChan
		if rp.debugOn {
			common.Debug("Received replica message", 0, rp.debugLevel, rp.debugOn)
		}

		switch replicaMessage.Code {

		case rp.messageCodes.StatusRPC:
			statusMessage := replicaMessage.Obj.(*proto.Status)
			if rp.debugOn {
				common.Debug("Status message from "+fmt.Sprintf("%#v", statusMessage.Sender), 0, rp.debugLevel, rp.debugOn)
			}
			rp.handleStatus(statusMessage)
			break

		case rp.messageCodes.ClientBatchRpc:
			clientBatch := replicaMessage.Obj.(*proto.ClientBatch)
			if rp.debugOn {
				common.Debug("Client batch message from "+fmt.Sprintf("%#v", clientBatch.Sender), 0, rp.debugLevel, rp.debugOn)
			}
			rp.handleClientBatch(clientBatch)
			break

		case rp.messageCodes.MemPoolRPC:
			memPoolMessage := replicaMessage.Obj.(*proto.MemPool)
			if rp.debugOn {
				common.Debug("Mem pool message from "+fmt.Sprintf("%#v", memPoolMessage.Sender), 0, rp.debugLevel, rp.debugOn)
			}
			rp.handleMemPool(memPoolMessage)
			break

		case rp.messageCodes.AsyncConsensus:
			asyncConsensusMessage := replicaMessage.Obj.(*proto.AsyncConsensus)
			if rp.debugOn {
				common.Debug("Async consensus message from "+fmt.Sprintf("%#v", asyncConsensusMessage.Sender), 0, rp.debugLevel, rp.debugOn)
			}
			rp.handleAsyncConsensus(asyncConsensusMessage)
			break

		case rp.messageCodes.PaxosConsensus:
			paxosConsensusMessage := replicaMessage.Obj.(*proto.PaxosConsensus)
			if rp.debugOn {
				common.Debug("Paxos consensus message from "+fmt.Sprintf("%#v", paxosConsensusMessage.Sender), 0, rp.debugLevel, rp.debugOn)
			}
			rp.handlePaxosConsensus(paxosConsensusMessage)
			break

		}
	}
}

/*
	Write a message to the wire, first the message type is written and then the actual message
*/

func (rp *Replica) internalSendMessage(peer int32, rpcPair *common.RPCPair) {
	peerType := rp.getNodeType(peer)
	if peerType == "replica" {
		if rpcPair.Code == rp.messageCodes.AsyncConsensus {
			asyncMessage := rpcPair.Obj.(*proto.AsyncConsensus)
			if asyncMessage.Type == 1 || asyncMessage.Type == 4 {
				time.Sleep(time.Duration(rp.networkBatchTime) * time.Millisecond)
			}
		}

		if rpcPair.Code == rp.messageCodes.PaxosConsensus {
			paxosMessage := rpcPair.Obj.(*proto.PaxosConsensus)
			if paxosMessage.Type == 3 {
				time.Sleep(time.Duration(rp.networkBatchTime) * time.Millisecond)
			}
		}

		w := rp.outgoingReplicaWriters[peer]
		if w == nil {
			panic("replica not found")
		}
		rp.outgoingReplicaWriterMutexs[peer].Lock()
		err := w.WriteByte(rpcPair.Code)
		if err != nil {
			if rp.debugOn {
				common.Debug("Error writing message code byte:"+err.Error(), 0, rp.debugLevel, rp.debugOn)
			}
			rp.outgoingReplicaWriterMutexs[peer].Unlock()
			return
		}
		err = rpcPair.Obj.Marshal(w)
		if err != nil {
			if rp.debugOn {
				common.Debug("Error while marshalling:"+err.Error(), 0, rp.debugLevel, rp.debugOn)
			}
			rp.outgoingReplicaWriterMutexs[peer].Unlock()
			return
		}
		err = w.Flush()
		if err != nil {
			if rp.debugOn {
				common.Debug("Error while flushing:"+err.Error(), 0, rp.debugLevel, rp.debugOn)
			}
			rp.outgoingReplicaWriterMutexs[peer].Unlock()
			return
		}
		rp.outgoingReplicaWriterMutexs[peer].Unlock()
		if rp.debugOn {
			common.Debug("Internal sent message to "+strconv.Itoa(int(peer)), 0, rp.debugLevel, rp.debugOn)
		}
	} else if peerType == "client" {
		w := rp.outgoingClientWriters[peer]
		if w == nil {
			panic("client not found")
		}
		rp.outgoingClientWriterMutexs[peer].Lock()
		err := w.WriteByte(rpcPair.Code)
		if err != nil {
			if rp.debugOn {
				common.Debug("Error writing message code byte:"+err.Error(), 0, rp.debugLevel, rp.debugOn)
			}
			rp.outgoingClientWriterMutexs[peer].Unlock()
			return
		}
		err = rpcPair.Obj.Marshal(w)
		if err != nil {
			if rp.debugOn {
				common.Debug("Error while marshalling:"+err.Error(), 0, rp.debugLevel, rp.debugOn)
			}
			rp.outgoingClientWriterMutexs[peer].Unlock()
			return
		}
		err = w.Flush()
		if err != nil {
			if rp.debugOn {
				common.Debug("Error while flushing:"+err.Error(), 0, rp.debugLevel, rp.debugOn)
			}
			rp.outgoingClientWriterMutexs[peer].Unlock()
			return
		}
		rp.outgoingClientWriterMutexs[peer].Unlock()
		if rp.debugOn {
			common.Debug("Internal sent message to "+strconv.Itoa(int(peer)), 0, rp.debugLevel, rp.debugOn)
		}
	} else {
		panic("Unknown id from node name " + strconv.Itoa(int(peer)))
	}
}

/*
	A set of threads that manages outgoing messages
*/

func (rp *Replica) StartOutgoingLinks() {
	for i := 0; i < numOutgoingThreads; i++ {
		go func() {
			for true {
				outgoingMessage := <-rp.outgoingMessageChan
				rp.internalSendMessage(outgoingMessage.Peer, outgoingMessage.RpcPair)
				if rp.debugOn {
					common.Debug("Invoked internal sent to replica "+strconv.Itoa(int(outgoingMessage.Peer)), 0, rp.debugLevel, rp.debugOn)
				}
			}
		}()
	}
}

/*
	Add a new out-going message to the outgoing channel
*/

func (rp *Replica) sendMessage(peer int32, rpcPair common.RPCPair) {
	rp.outgoingMessageChan <- &common.OutgoingRPC{
		RpcPair: &rpcPair,
		Peer:    peer,
	}
	if rp.debugOn {
		common.Debug("Added RPC pair to outgoing channel to peer "+strconv.Itoa(int(peer)), 0, rp.debugLevel, rp.debugOn)
	}
}
