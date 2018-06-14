package main

import (
	"bytes"
	"errors"
	"net"
	"time"

	"github.com/paha/go-tftp/wire"
)

const maxDataSize = 512

var (
	errorCodes = map[uint16]string{
		0: "Not defined, see error message (if any).",
		1: "File not found.",
		2: "Access violation",
		3: "Disk full or allocation exceeded.",
		4: "Illegal TFTP operation.",
		5: "Unknown transfer ID.",
		6: "File already exists.",
		7: "No such user.",
	}
)

// transfer defines active file transfer
// NOTE:
// 	1. Used as an object for the store. Waist of space though.
//	2. For outgoing active transfers host:port string is used instead of a
// 		filename to provide uniq identifier.
type transfer struct {
	lastOp   time.Time
	addr     net.Addr
	conn     net.PacketConn
	blockNum uint16
	filename string
	data     []byte
	sendData []byte
	retry    bool
}

// send serializes packet and calls transmit function
func (t *transfer) send(r tftp.Packet) {
	t.sendData = r.Serialize()
	t.transmit()
}

// sendD transmits serialized data
func (t *transfer) transmit() {
	_, err := t.conn.WriteTo(t.sendData, t.addr)
	if err != nil {
		logger.Printf("Error: failed sending a packet. %s", err)
		t.retry = true
	} else {
		t.retry = false
		t.lastOp = time.Now()
	}
}

// wrqHandler creates a new inFlight transfer and responds with Ack
func wrqHandler(conn net.PacketConn, p tftp.Packet, addr net.Addr) {
	pkt := p.(*tftp.PacketRequest)

	lock.Lock()
	_, ok := store[pkt.Filename]
	lock.Unlock()
	if ok {
		logger.Printf("%s already exists. Client %v", pkt.Filename, addr)
		registryLogger.Printf("WRQ for %s from %v. ERR: file already exists.", pkt.Filename, addr)
		sendError(6, addr, conn)
		return
	}
	logger.Printf("Receiving %s from %v", pkt.Filename, addr)
	registryLogger.Printf("WRQ for %s from %v. Receiving...", pkt.Filename, addr)

	t := transfer{
		blockNum: 1,
		addr:     addr,
		conn:     conn,
		filename: pkt.Filename,
	}
	res := tftp.PacketAck{BlockNum: 0}
	t.send(&res)

	lock.Lock()
	inFlight[t.filename] = t
	lock.Unlock()
}

// rrqHandlers checks if requested file is in the store, returns error to RFC1350
// if it isn't, otherwise sends the first data block.
func rrqHandler(conn net.PacketConn, p tftp.Packet, addr net.Addr) {
	pkt := p.(*tftp.PacketRequest)

	lock.Lock()
	t, ok := store[pkt.Filename]
	lock.Unlock()
	if !ok {
		logger.Printf("%s isn't found. Client %v", pkt.Filename, addr)
		registryLogger.Printf("RRQ for %s from %v. ERR: not found.", pkt.Filename, addr)
		sendError(1, addr, conn)
		return
	}
	logger.Printf("Sending %s to %v", t.filename, addr)
	registryLogger.Printf("RRQ for %s from %v. Sending...", pkt.Filename, addr)

	t.blockNum = 1
	t.addr = addr
	t.conn = conn
	t.filename = addr.String()
	var data []byte
	if len(t.data) >= maxDataSize {
		data = t.data[:maxDataSize]
	} else {
		data = t.data
	}
	res := tftp.PacketData{Data: data, BlockNum: t.blockNum}
	t.send(&res)

	lock.Lock()
	inFlight[t.filename] = t
	lock.Unlock()
}

// dataHandler process incoming data block and stores the file if the
// transfer is complete, removing it from the inFlight
func dataHandler(conn net.PacketConn, p tftp.Packet, addr net.Addr, n int) {
	pkt := p.(*tftp.PacketData)

	t, err := findTransfer(pkt.BlockNum, addr)
	if err != nil {
		logger.Printf("Error: %s", err)
		sendError(5, addr, conn)
		return
	}

	// Trim NULL characters
	d := bytes.Trim(pkt.Data, "\x00")
	// TODO: investigate where the extra bytes on the last block come from,
	//		had to limit to expected size of data (d[:n])
	t.data = append(t.data, d[:n]...)
	t.blockNum++
	res := tftp.PacketAck{BlockNum: pkt.BlockNum}
	t.send(&res)

	lock.Lock()
	if n < maxDataSize {
		store[t.filename] = t
		delete(inFlight, t.filename)
		logger.Printf("Finished receiving %s from %v", t.filename, addr)
	} else {
		inFlight[t.filename] = t
	}
	lock.Unlock()
}

// ackHandler processes ack for ongoing transfer and sends the next data block
func ackHandler(conn net.PacketConn, p tftp.Packet, addr net.Addr) {
	pkt := p.(*tftp.PacketAck)

	t, err := findTransfer(pkt.BlockNum, addr)
	if err != nil {
		return
	}

	t.blockNum++
	tsize := t.blockNum * maxDataSize
	rsize := pkt.BlockNum * maxDataSize
	var data []byte
	if len(t.data) >= int(tsize) {
		data = t.data[rsize:tsize]
	} else if len(t.data) < int(rsize) {
		// Leaving the transfer in inFlight in case of pending retransmits.
		logger.Printf("Transfer is complete %s", t.filename)
		lock.Lock()
		delete(inFlight, t.filename)
		lock.Unlock()
		return
	} else {
		data = t.data[rsize:]
	}

	res := tftp.PacketData{BlockNum: t.blockNum, Data: data}
	t.send(&res)

	lock.Lock()
	inFlight[t.filename] = t
	lock.Unlock()
}

func errHandler(p tftp.Packet, addr net.Addr) {
	pkt := p.(*tftp.PacketError)
	logger.Printf("Received an error from %v: %d : %s", addr, pkt.Code, pkt.Msg)
}

func sendError(code uint16, addr net.Addr, conn net.PacketConn) {
	logger.Printf("Sending err %s to %v", errorCodes[code], addr)
	res := tftp.PacketError{Code: code, Msg: errorCodes[code]}
	_, err := conn.WriteTo(res.Serialize(), addr)
	if err != nil {
		logger.Printf("Error sending err to %v %s", addr, err)
	}
}

func findTransfer(block uint16, addr net.Addr) (t transfer, err error) {
	lock.Lock()
	defer lock.Unlock()
	for _, t := range inFlight {
		if t.blockNum == block && addr.String() == t.addr.String() {
			return t, nil
		}
	}
	return transfer{}, errors.New("unknown transfer")
}
