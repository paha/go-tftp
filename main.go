/*
	A simple in-memory TFTP server to RFC1350.
	NO implementation of followup/update RFCs!!!

TODO:
	* Dynamically set a limit for the store based on available memory
	* Consider using WriteTo and ReadFrom Deadlines, re: https://golang.org/pkg/net/#PacketConn
	* Track errors on active transmissions, add logic to handle better some of edge cases
*/

package main

import (
	"encoding/binary"
	"flag"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/paha/go-tftp/wire"
)

var (
	host          string
	rexmtInterval int
	maxTimeout    int
	registryFile  string

	inFlight       map[string]transfer
	store          map[string]transfer
	logger         *log.Logger
	registryLogger *log.Logger

	lock = sync.RWMutex{}
)

func init() {
	flag.StringVar(&host, "host", "127.0.0.1:6969", "TFTP interface address")
	flag.IntVar(&rexmtInterval, "Rexmt-interval", 5, "TFTP Retransmit interval")
	flag.IntVar(&maxTimeout, "Max-timeout", 25, "TFTP max timeout")
	flag.StringVar(&registryFile, "registryFile", "tftpRegistry.log", "TFTP WRQ/RRQ registry.")

	// Active transfers.
	inFlight = make(map[string]transfer)
	// TFTP datasotore.
	store = make(map[string]transfer)
}

func main() {
	flag.Parse()

	conn, err := net.ListenPacket("udp", host)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	logger = log.New(os.Stdout, "tftp: ", log.Lshortfile)
	logger.Printf("Started on %v", conn.LocalAddr())

	newRegistryLogger() // Each transfer recorded to a file.
	registryLogger.Print("Server started.")

	go flush(500) // 500 milliseconds ticks period
	for {
		buf := make([]byte, tftp.MaxPacketSize)
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			logger.Printf("Error: %s", err)
			continue
		}

		go newPacket(conn, addr, buf, n)
	}
}

func newPacket(conn net.PacketConn, addr net.Addr, buf []byte, n int) {
	p, err := tftp.ParsePacket(buf)
	if err != nil {
		logger.Printf("Error parsing a packet: %s", err)
		sendError(0, addr, conn)
		return
	}

	op := binary.BigEndian.Uint16(buf)
	switch op {
	case tftp.OpWRQ:
		wrqHandler(conn, p, addr)
	case tftp.OpRRQ:
		rrqHandler(conn, p, addr)
	case tftp.OpAck:
		ackHandler(conn, p, addr)
	case tftp.OpData:
		dataHandler(conn, p, addr, n-4)
	case tftp.OpError:
		errHandler(p, addr)
	default:
		logger.Printf("Error: Unrecognized OpCode - %d.", op)
		sendError(0, addr, conn)
	}
}

// flush deletes expired pending transfers, and handles retries.
func flush(m int16) {
	ticker := time.NewTicker(time.Millisecond * time.Duration(m))
	for range ticker.C {
		lock.Lock()
		for _, t := range inFlight {
			d := time.Duration(time.Second * time.Duration(maxTimeout))
			if d < time.Now().Sub(t.lastOp) {
				logger.Printf("Transfer of %s has expired.", t.filename)
				delete(inFlight, t.filename)
			} else if t.retry {
				td := time.Duration(time.Second * time.Duration(rexmtInterval))
				if td < time.Now().Sub(t.lastOp) {
					logger.Printf("Retransmiting last packet for %s", t.filename)
					t.transmit()
				}
			}
		}
		lock.Unlock()
	}
}

func newRegistryLogger() {
	lfh, err := os.OpenFile(registryFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	logger.Printf("Registry file %s", registryFile)
	if err != nil {
		log.Fatal(err)
	}
	registryLogger = log.New(lfh, "TFTP registry: ", log.LstdFlags)
}
