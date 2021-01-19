package ratp

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

type mockSerialPort struct {
	read  func([]byte) (int, error)
	write func([]byte) (int, error)
	close func() error
}

func (s mockSerialPort) Read(data []byte) (int, error) {
	if s.read == nil {
		time.Sleep(100 * time.Millisecond)
		return 0, io.EOF
	}
	return s.read(data)
}

func (s mockSerialPort) Write(p []byte) (int, error) {
	if s.write == nil {
		return len(p), nil
	}
	return s.write(p)
}

func (s mockSerialPort) Close() error {
	if s.close == nil {
		return nil
	}
	return s.close()
}

func createMockConnection(mdl int) *Connection {
	c := createFilledConnection()
	c.mdl = mdl
	c.log = nilLogger{}
	c.port = mockSerialPort{}
	return c
}

func TestEstablishConnection(t *testing.T) {
	// 3 possible outcomes
	for i := 0; i < 3; i++ {
		c := createMockConnection(0x42)

		switch {
		case c.state != stateClosed:
			t.Error()
		case c.getState == nil:
			t.Error()
		case c.setState == nil:
			t.Error()
		case c.getMDL == nil:
			t.Error()
		case c.setMDL == nil:
			t.Error()
		case c.stateRoutineTermination == nil:
			t.Error()
		case c.sendRoutineTermination == nil:
			t.Error()
		case c.outgoingPackets == nil:
			t.Error()
		case c.sendRoutineEvent == nil:
			t.Error()
		case c.sendRoutineError == nil:
			t.Error()
		case c.rto != 350*time.Millisecond:
			t.Error()
		case c.recvRoutineTermination == nil:
			t.Error()
		case c.recvConnEstablished == nil:
			t.Error()
		case c.recvRoutineError == nil:
			t.Error()
		case c.recvTxData == nil:
			t.Error()
		case c.recvRxData == nil:
			t.Error()
		case c.recvBytesRead == nil:
			t.Error()
		case !c.emptyTx:
			t.Error()
		case c.recvSN != 0:
			t.Error()
		case c.sendSN != 1:
			t.Error()
		}

		var lock sync.Mutex
		lock.Lock()
		var err error
		go func() { defer lock.Unlock(); _, err = establishConnection(c) }()

		if stateSynSent != <-c.setState {
			t.Error()
		}

		p := <-c.outgoingPackets
		orig := []byte{0x01, 0x80, 0x42, 0x3D}
		if !matchPacket(p, orig) {
			t.Errorf("%X != %X\n", p, orig)
		}

		switch i {
		case 0:
			customError := errors.New("Custom error")
			c.recvRoutineError <- customError

			lock.Lock()
			if err != customError {
				t.Error()
			}

		case 1:
			customError := errors.New("Custom error")
			c.sendRoutineError <- customError

			lock.Lock()
			if err != customError {
				t.Error()
			}

		case 2:
			c.recvConnEstablished <- struct{}{}

			lock.Lock()
			if err != nil {
				t.Error()
			}
		}
	}
}

func TestStateToStr(t *testing.T) {
	switch {
	case stateToStr(stateListen) != "LISTEN":
		t.Error()
	case stateToStr(stateSynSent) != "SYN-SENT":
		t.Error()
	case stateToStr(stateAltEstablished) != "ESTABLISHED":
		t.Error()
	case stateToStr(stateEstablished) != "ESTABLISHED":
		t.Error()
	case stateToStr(stateFinWait) != "FIN-WAIT":
		t.Error()
	case stateToStr(stateLastAck) != "LAST-ACK":
		t.Error()
	case stateToStr(stateClosing) != "CLOSING":
		t.Error()
	case stateToStr(stateTimeWait) != "TIME-WAIT":
		t.Error()
	case stateToStr(stateClosed) != "CLOSED":
		t.Error()
	case stateToStr(42) != "UNKNOWN":
		t.Error()
	}
}

func TestStatRoutine(t *testing.T) {
	c := createMockConnection(0x42)
	startRoutines(c)
	defer c.Close()

	if <-c.getState != stateClosed {
		t.Error()
	}
	for i := 0; i <= stateClosed; i++ {
		c.setState <- i
		if <-c.getState != i {
			t.Error()
		}
	}

	if <-c.getMDL != 0x42 {
		t.Error()
	}
	for i := 1; i < 256; i++ {
		c.setMDL <- i
		if <-c.getMDL != i {
			t.Error()
		}
	}
}

func TestHandlePacketImpossibleState(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error()
		}
	}()

	c := createMockConnection(256)
	go func() { c.getState <- 42 }()
	c.handlePacket(makePacketWithoutData(0, 0))
}

func TestSendNextData(t *testing.T) {
	c := createMockConnection(256)
	var lock sync.Mutex
	lock.Lock()

	// len(data) less then MDL
	data := []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66}
	c.txBuf = data
	c.sendSN = 0
	c.recvSN = 0
	go func() { defer lock.Unlock(); c.sendNextData() }()
	c.getMDL <- 255
	out := <-c.outgoingPackets
	lock.Lock()
	if (out.flags()&flagSN) != 0 || (out.flags()&flagAN) != 0 || (out.flags()&flagACK) == 0 ||
		c.sendSN != 1 || int(out[posLength]) != len(data) ||
		bytes.Compare(out[posData:len(out)-2], data) != 0 {
		t.Error()
	}
	c.txBuf = data
	c.sendSN = 0
	c.recvSN = 1
	go func() { defer lock.Unlock(); c.sendNextData() }()
	c.getMDL <- 255
	out = <-c.outgoingPackets
	lock.Lock()
	if (out.flags()&flagSN) != 0 || (out.flags()&flagAN) == 0 || (out.flags()&flagACK) == 0 ||
		c.sendSN != 1 || int(out[posLength]) != len(data) ||
		bytes.Compare(out[posData:len(out)-2], data) != 0 {
		t.Error()
	}
	c.txBuf = data
	c.sendSN = 1
	c.recvSN = 0
	go func() { defer lock.Unlock(); c.sendNextData() }()
	c.getMDL <- 255
	out = <-c.outgoingPackets
	lock.Lock()
	if (out.flags()&flagSN) == 0 || (out.flags()&flagAN) != 0 || (out.flags()&flagACK) == 0 ||
		c.sendSN != 2 || int(out[posLength]) != len(data) ||
		bytes.Compare(out[posData:len(out)-2], data) != 0 {
		t.Error()
	}
	c.txBuf = data
	c.sendSN = 1
	c.recvSN = 1
	go func() { defer lock.Unlock(); c.sendNextData() }()
	c.getMDL <- 255
	out = <-c.outgoingPackets
	lock.Lock()
	if (out.flags()&flagSN) == 0 || (out.flags()&flagAN) == 0 || (out.flags()&flagACK) == 0 ||
		c.sendSN != 2 || int(out[posLength]) != len(data) ||
		bytes.Compare(out[posData:len(out)-2], data) != 0 {
		t.Error()
	}

	// len(data) greater then MDL
	c.txBuf = data
	c.sendSN = 0
	c.recvSN = 0
	go func() { defer lock.Unlock(); c.sendNextData() }()
	c.getMDL <- 4
	out = <-c.outgoingPackets
	lock.Lock()
	if (out.flags()&flagSN) != 0 || (out.flags()&flagAN) != 0 || (out.flags()&flagACK) == 0 ||
		c.sendSN != 1 || int(out[posLength]) != 4 ||
		bytes.Compare(out[posData:len(out)-2], data[:4]) != 0 {
		t.Error()
	}
}

func TestBehaviorA(t *testing.T) {
	c := createMockConnection(256)
	var p packet
	var needContinue bool
	var err error
	var lock sync.Mutex
	lock.Lock()

	// Behavior A not implemented
	p = makePacketWithoutData(flagSYN, 0xFF)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorA(p) }()
	out := <-c.outgoingPackets
	lock.Lock()
	if needContinue || err != ErrNotImpl || (out.flags()&flagRST) == 0 ||
		(out.flags()&flagSN) != 0 {
		t.Error()
	}
}

func TestBehaviorB(t *testing.T) {
	c := createMockConnection(256)
	var p packet
	var needContinue bool
	var err error
	var lock sync.Mutex
	lock.Lock()

	// First, check the packet for the ACK flag.  If the ACK flag is
	// set then check to see if the AN value was as expected.  If it
	// was continue below.  Otherwise the AN value was unexpected.  If
	// the RST flag was set then discard the packet and return to the
	// current state without any further processing…
	p = makePacketWithoutData(flagACK|flagRST, 0xFF) // AN is unexpected
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorB(p) }()
	lock.Lock()
	if needContinue || err != nil {
		t.Error()
	}

	// … else send a reset:
	// <SN=received AN><CTL=RST>
	// Discard the packet and return to the current state without any
	// further processing.
	p = makePacketWithoutData(flagACK, 0xFF) // AN is unexpected
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorB(p) }()
	out := <-c.outgoingPackets
	lock.Lock()
	if needContinue || err != nil || (out.flags()&flagRST) == 0 ||
		(out.flags()&flagSN) != 0 {
		t.Error()
	}

	// At this point either the ACK flag was set and the AN value was
	// as expected or ACK was not set.  Second, check the RST flag.
	// If the RST flag is set there are two cases:
	//    1. If the ACK flag is set then discard the packet, flush the
	//    retransmission queue, inform the user "Error: Connection
	//    refused", delete the TCB, and go to the CLOSED state without
	//    any further processing.
	p = makePacketWithoutData(flagACK|flagRST|flagAN, 0xFF)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorB(p) }()
	sendEvt := <-c.sendRoutineEvent
	newState := <-c.setState
	lock.Lock()
	if needContinue || err != ErrConnRefused || sendEvt != discardPacket ||
		newState != stateClosed {
		t.Error()
	}

	//    2. If the ACK flag was not set then discard the packet and
	//    return to this state without any further processing.
	p = makePacketWithoutData(flagRST|flagAN, 0xFF)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorB(p) }()
	lock.Lock()
	if needContinue || err != nil {
		t.Error()
	}

	// At this point we assume the packet contained an ACK which was
	// Ok, or there was no ACK, and there was no RST.  Now check the
	// packet for the SYN flag.  If the ACK flag was set then our SYN
	// has been acknowledged.  Store MDL received in the TCB.  At this
	// point we are technically in the ESTABLISHED state.
	for i := 0; i < 4; i++ { // all SN/AN combinations
		if i == 0 || i == 1 {
			c.sendSN = 0
		} else {
			c.sendSN = 1
		}
		var flags flags = flagSYN | flagACK
		if i == 2 || i == 3 {
			flags |= flagAN
		}
		if i == 1 || i == 3 {
			flags |= flagSN
		}
		p = makePacketWithoutData(flags, 0x42)
		go func() { defer lock.Unlock(); needContinue, err = c.behaviorB(p) }()
		newMDL := <-c.setMDL
		sendEvt = <-c.sendRoutineEvent
		newState = <-c.setState
		<-c.recvConnEstablished
		lock.Lock()
		expectedSN := 0
		if i == 0 || i == 2 {
			expectedSN = 1
		}
		expectedAN := 0
		if i == 2 || i == 3 {
			expectedAN = 1
		}
		if needContinue || err != nil || newMDL != 0x42 || sendEvt != discardPacket ||
			newState != stateAltEstablished || c.sendSN != expectedAN || c.recvSN != expectedSN {
			t.Error()
		}
	}

	// If the SYN flag was set but the ACK was not set then the other
	// end of the connection has executed an active open also.
	// Case not supported.
	p = makePacketWithoutData(flagSYN|flagAN, 0xFF)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorB(p) }()
	sendEvt = <-c.sendRoutineEvent
	newState = <-c.setState
	lock.Lock()
	if needContinue || err != ErrNotImpl || sendEvt != discardPacket || newState != stateClosed {
		t.Error()
	}
}

func TestBehaviorC1(t *testing.T) {
	c := createMockConnection(256)
	var p packet
	var needContinue bool
	var err error
	var lock sync.Mutex
	lock.Lock()

	// Behavior A not implemented
	p = makePacketWithoutData(0, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorC1(p) }()
	out := <-c.outgoingPackets
	lock.Lock()
	if needContinue || err != ErrNotImpl || (out.flags()&flagRST) == 0 ||
		(out.flags()&flagSN) != 0 {
		t.Error()
	}
}

func TestBehaviorC2(t *testing.T) {
	c := createMockConnection(256)
	var p packet
	var needContinue bool
	var err error
	var lock sync.Mutex
	lock.Lock()

	// Examine the received SN field value.  If the SN value was
	// expected then return and continue the processing associated
	// with this state.
	p = makePacketWithoutData(0, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorC2(p) }()
	lock.Lock()
	if !needContinue || err != nil {
		t.Error()
	}

	// We now assume the SN value was not what was expected.
	// If either RST or FIN were set discard the packet and return to
	// the current state without any further processing.
	p = makePacketWithoutData(flagRST|flagSN, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorC2(p) }()
	lock.Lock()
	if needContinue || err != nil {
		t.Error()
	}
	p = makePacketWithoutData(flagFIN|flagSN, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorC2(p) }()
	lock.Lock()
	if needContinue || err != nil {
		t.Error()
	}
	p = makePacketWithoutData(flagRST|flagFIN|flagSN, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorC2(p) }()
	lock.Lock()
	if needContinue || err != nil {
		t.Error()
	}

	// If SYN was set we assume that the other end crashed and has
	// attempted to open a new connection.  We respond by sending a
	// legal reset:
	//   <SN=received AN><AN=received SN+1 modulo 2><CTL=RST, ACK>
	// This will cause the other end, currently in the SYN-SENT state,
	// to close.  Flush the retransmission queue, inform the user
	// "Error: Connection reset", discard the packet, delete the TCB,
	// and go to the CLOSED state without any further processing.
	c.recvSN = 1
	p = makePacketWithoutData(flagSYN, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorC2(p) }()
	sendEvt := <-c.sendRoutineEvent
	out := <-c.outgoingPackets
	newState := <-c.setState
	lock.Lock()
	if needContinue || err != ErrConnReset || sendEvt != discardPacket ||
		newState != stateClosed || (out.flags()&flagSN) != 0 || (out.flags()&flagAN) == 0 {
		t.Error()
	}
	p = makePacketWithoutData(flagSYN|flagAN, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorC2(p) }()
	sendEvt = <-c.sendRoutineEvent
	out = <-c.outgoingPackets
	newState = <-c.setState
	lock.Lock()
	if needContinue || err != ErrConnReset || sendEvt != discardPacket ||
		newState != stateClosed || (out.flags()&flagSN) == 0 || (out.flags()&flagAN) == 0 {
		t.Error()
	}
	c.recvSN = 0
	p = makePacketWithoutData(flagSYN|flagSN, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorC2(p) }()
	sendEvt = <-c.sendRoutineEvent
	out = <-c.outgoingPackets
	newState = <-c.setState
	lock.Lock()
	if needContinue || err != ErrConnReset || sendEvt != discardPacket ||
		newState != stateClosed || (out.flags()&flagSN) != 0 || (out.flags()&flagAN) != 0 {
		t.Error()
	}
	p = makePacketWithoutData(flagSYN|flagSN|flagAN, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorC2(p) }()
	sendEvt = <-c.sendRoutineEvent
	out = <-c.outgoingPackets
	newState = <-c.setState
	lock.Lock()
	if needContinue || err != ErrConnReset || sendEvt != discardPacket ||
		newState != stateClosed || (out.flags()&flagSN) == 0 || (out.flags()&flagAN) != 0 {
		t.Error()
	}

	// If neither RST, FIN, nor SYN flags were set it is assumed that
	// this packet is a duplicate of one already received.  Send an
	// ACK back:
	//    <SN=received AN><AN=received SN+1 modulo 2><CTL=ACK>
	// Discard the duplicate packet and return to the current state
	// without any further processing.
	c.recvSN = 1
	p, _ = makePacketWithData(0, []byte{0x42})
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorC2(p) }()
	out = <-c.outgoingPackets
	lock.Lock()
	if needContinue || err != nil || (out.flags()&flagACK) == 0 || (out.flags()&flagSN) != 0 ||
		(out.flags()&flagAN) == 0 {
		t.Error()
	}
	p, _ = makePacketWithData(flagAN, []byte{0x42})
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorC2(p) }()
	out = <-c.outgoingPackets
	lock.Lock()
	if needContinue || err != nil || (out.flags()&flagACK) == 0 || (out.flags()&flagSN) == 0 ||
		(out.flags()&flagAN) == 0 {
		t.Error()
	}
	c.recvSN = 0
	p, _ = makePacketWithData(flagSN, []byte{0x42})
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorC2(p) }()
	out = <-c.outgoingPackets
	lock.Lock()
	if needContinue || err != nil || (out.flags()&flagACK) == 0 || (out.flags()&flagSN) != 0 ||
		(out.flags()&flagAN) != 0 {
		t.Error()
	}
	p, _ = makePacketWithData(flagSN|flagAN, []byte{0x42})
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorC2(p) }()
	out = <-c.outgoingPackets
	lock.Lock()
	if needContinue || err != nil || (out.flags()&flagACK) == 0 || (out.flags()&flagSN) == 0 ||
		(out.flags()&flagAN) != 0 {
		t.Error()
	}
}

func TestBehaviorD1(t *testing.T) {
	// Behavior D1 not implemented
	defer func() {
		if r := recover(); r == nil {
			t.Error()
		}
	}()

	c := createMockConnection(256)
	c.behaviorD1(packet{})
}

func TestBehaviorD2(t *testing.T) {
	c := createMockConnection(256)
	var p packet
	var needContinue bool
	var err error
	var lock sync.Mutex
	lock.Lock()

	// The packet is examined for a RST flag.  If RST is not set then
	// return and continue the processing associated with this state.
	p = makePacketWithoutData(0, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorD2(p) }()
	lock.Lock()
	if !needContinue || err != nil {
		t.Error()
	}

	// RST is now assumed to have been set.  Any data remaining to be
	// sent is flushed.  The retransmission queue is flushed, the user
	// is informed "Error: Connection reset.", discard the packet,
	// delete the TCB, and go to the CLOSED state without any further
	// processing.
	p = makePacketWithoutData(flagRST, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorD2(p) }()
	sendEvt := <-c.sendRoutineEvent
	newState := <-c.setState
	lock.Lock()
	if needContinue || err != ErrConnReset || sendEvt != discardPacket ||
		newState != stateClosed {
		t.Error()
	}
}

func TestBehaviorD3(t *testing.T) {
	c := createMockConnection(256)
	var p packet
	var needContinue bool
	var err error
	var lock sync.Mutex
	lock.Lock()

	// The packet is examined for a RST flag.  If RST is not set then
	// return and continue the processing associated with this state.
	p = makePacketWithoutData(0, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorD3(p) }()
	lock.Lock()
	if !needContinue || err != nil {
		t.Error()
	}

	// RST is now assumed to have been set.  Discard the packet,
	// delete the TCB, and go to the CLOSED state without any further
	// processing.
	p = makePacketWithoutData(flagRST, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorD3(p) }()
	sendEvt := <-c.sendRoutineEvent
	newState := <-c.setState
	lock.Lock()
	if needContinue || err != ErrConnReset || sendEvt != discardPacket ||
		newState != stateClosed {
		t.Error()
	}
}

func TestBehaviorE(t *testing.T) {
	c := createMockConnection(256)
	var p packet
	var needContinue bool
	var err error
	var lock sync.Mutex
	lock.Lock()

	// Check the presence of the SYN flag.  If the SYN flag is not set
	// then return and continue the processing associated with this
	// state.
	p = makePacketWithoutData(0, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorE(p) }()
	lock.Lock()
	if !needContinue || err != nil {
		t.Error()
	}

	// We now assume that the SYN flag was set.  The presence of a SYN
	// here is an error.  Flush the retransmission queue, send a legal
	// RST packet.
	// The user should receive the message "Error: Connection reset.",
	// then delete the TCB and go to the CLOSED state without any
	// further processing.
	//    If the ACK flag was not set then send:
	//       <SN=0><CTL=RST>
	p = makePacketWithoutData(flagSYN, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorE(p) }()
	sendEvt := <-c.sendRoutineEvent
	out := <-c.outgoingPackets
	newState := <-c.setState
	lock.Lock()
	if needContinue || err != ErrConnReset || sendEvt != discardPacket ||
		newState != stateClosed || (out.flags()&flagSN) != 0 || (out.flags()&flagRST) == 0 {
		t.Error()
	}

	//    If the ACK flag was set then send:
	//       <SN=received AN><CTL=RST>
	p = makePacketWithoutData(flagSYN|flagACK, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorE(p) }()
	sendEvt = <-c.sendRoutineEvent
	out = <-c.outgoingPackets
	newState = <-c.setState
	lock.Lock()
	if needContinue || err != ErrConnReset || sendEvt != discardPacket ||
		newState != stateClosed || (out.flags()&flagSN) != 0 || (out.flags()&flagRST) == 0 {
		t.Error()
	}
	p = makePacketWithoutData(flagSYN|flagACK|flagAN, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorE(p) }()
	sendEvt = <-c.sendRoutineEvent
	out = <-c.outgoingPackets
	newState = <-c.setState
	lock.Lock()
	if needContinue || err != ErrConnReset || sendEvt != discardPacket ||
		newState != stateClosed || (out.flags()&flagSN) == 0 || (out.flags()&flagRST) == 0 {
		t.Error()
	}
}

func TestBehaviorF1(t *testing.T) {
	// Behavior F1 not implemented
	defer func() {
		if r := recover(); r == nil {
			t.Error()
		}
	}()

	c := createMockConnection(256)
	c.behaviorF1(packet{})
}

func TestBehaviorF2(t *testing.T) {
	c := createMockConnection(256)
	var p packet
	var needContinue bool
	var err error
	var lock sync.Mutex
	lock.Lock()

	// Check the presence of the ACK flag.  If ACK is not set then
	// discard the packet and return without any further processing.
	p = makePacketWithoutData(0, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorF2(p) }()
	lock.Lock()
	if needContinue || err != nil {
		t.Error()
	}

	// We now assume that the ACK flag was set.  If the AN field value
	// was as expected then flush the retransmission queue and inform
	// the user with an "Ok" if a buffer has been entirely
	// acknowledged.  Another packet containing data may now be sent.
	// Return and continue the processing associated with this state.
	p = makePacketWithoutData(flagACK|flagAN, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorF2(p) }()
	lock.Lock()
	if !needContinue || err != nil {
		t.Error()
	}

	// We now assume that the ACK flag was set and that the AN field
	// value was unexpected.  This is assumed to indicate a duplicate
	// acknowledgment.  It is ignored, return and continue the
	// processing associated with this state.
	// Extension: Duplicate acknowledgment treated as "need to wait" mark.
	p = makePacketWithoutData(flagACK, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorF2(p) }()
	sendEvt := <-c.sendRoutineEvent
	lock.Lock()
	if needContinue || err != nil || sendEvt != restartRetransmission {
		t.Error()
	}
}

func TestBehaviorF3(t *testing.T) {
	c := createMockConnection(256)
	var p packet
	var needContinue bool
	var err error
	var lock sync.Mutex
	lock.Lock()

	// Check the presence of the ACK flag.  If ACK is not set then
	// discard the packet and return without any further processing.
	p = makePacketWithoutData(0, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorF3(p) }()
	lock.Lock()
	if needContinue || err != nil {
		t.Error()
	}

	// We now assume that the ACK flag was set.  If the AN field value
	// was as expected then continue the processing associated with
	// this state.
	p = makePacketWithoutData(flagACK|flagAN, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorF3(p) }()
	lock.Lock()
	if !needContinue || err != nil {
		t.Error()
	}

	// We now assume that the ACK flag was set and that the AN field
	// value was unexpected.  This is ignored, return and continue
	// with the processing associated with this state.
	p = makePacketWithoutData(flagACK, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorF3(p) }()
	lock.Lock()
	if !needContinue || err != nil {
		t.Error()
	}
}

func TestBehaviorG(t *testing.T) {
	c := createMockConnection(256)
	var p packet
	var needContinue bool
	var err error
	var lock sync.Mutex
	lock.Lock()

	// This procedure represents the behavior of the CLOSED state of a
	// connection.  All incoming packets are discarded.  If the packet
	// had the RST flag set take no action.
	p = makePacketWithoutData(flagRST, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorG(p) }()
	lock.Lock()
	if needContinue || err != nil {
		t.Error()
	}

	//                                      Otherwise it is necessary
	// to build a RST packet.  Since this end is closed the other end
	// of the connection has incorrect data about the state of the
	// connection and should be so informed.
	// After sending the reset packet return to the current state
	// without any further processing.
	//    If the ACK flag was set then send:
	//       <SN=received AN><CTL=RST>
	p = makePacketWithoutData(flagACK, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorG(p) }()
	out := <-c.outgoingPackets
	lock.Lock()
	if needContinue || err != nil || (out.flags()&flagRST) == 0 || (out.flags()&flagSN) != 0 {
		t.Error()
	}
	p = makePacketWithoutData(flagACK|flagAN, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorG(p) }()
	out = <-c.outgoingPackets
	lock.Lock()
	if needContinue || err != nil || (out.flags()&flagRST) == 0 || (out.flags()&flagSN) == 0 {
		t.Error()
	}

	//    If the ACK flag was not set then send:
	//       <SN=0><AN=received SN+1 modulo 2><CTL=RST, ACK>
	p = makePacketWithoutData(0, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorG(p) }()
	out = <-c.outgoingPackets
	lock.Lock()
	if needContinue || err != nil || (out.flags()&flagRST) == 0 || (out.flags()&flagACK) == 0 ||
		(out.flags()&flagSN) != 0 || (out.flags()&flagAN) == 0 {
		t.Error()
	}
	p = makePacketWithoutData(flagSN, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorG(p) }()
	out = <-c.outgoingPackets
	lock.Lock()
	if needContinue || err != nil || (out.flags()&flagRST) == 0 || (out.flags()&flagACK) == 0 ||
		(out.flags()&flagSN) != 0 || (out.flags()&flagAN) != 0 {
		t.Error()
	}
}

func TestBehaviorH1(t *testing.T) {
	// Behavior H1 not implemented
	defer func() {
		if r := recover(); r == nil {
			t.Error()
		}
	}()

	c := createMockConnection(256)
	c.behaviorH1(packet{})
}

func TestBehaviorH2(t *testing.T) {
	c := createMockConnection(256)
	var p packet
	var needContinue bool
	var err error
	var lock sync.Mutex
	lock.Lock()

	// Check the presence of the FIN flag.  If FIN is not set then
	// continue the processing associated with this state.
	p = makePacketWithoutData(0, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorH2(p) }()
	lock.Lock()
	if !needContinue || err != nil {
		t.Error()
	}

	// We now assume that the FIN flag was set.  This means the other
	// end has decided to close the connection.  Flush the
	// retransmission queue.  An acknowledgment for
	// the FIN must be sent which also indicates this end is closing:
	//    <SN=received AN><AN=received SN + 1 modulo 2><CTL=FIN, ACK>
	// Go to the LAST-ACK state without any further processing.
	p = makePacketWithoutData(flagFIN, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorH2(p) }()
	sendEvt := <-c.sendRoutineEvent
	out := <-c.outgoingPackets
	newState := <-c.setState
	lock.Lock()
	if needContinue || err != nil || sendEvt != discardPacket || newState != stateLastAck ||
		(out.flags()&flagFIN) == 0 || (out.flags()&flagACK) == 0 || (out.flags()&flagSN) != 0 ||
		(out.flags()&flagAN) == 0 {
		t.Error()
	}
	p = makePacketWithoutData(flagFIN|flagSN, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorH2(p) }()
	sendEvt = <-c.sendRoutineEvent
	out = <-c.outgoingPackets
	newState = <-c.setState
	lock.Lock()
	if needContinue || err != nil || sendEvt != discardPacket || newState != stateLastAck ||
		(out.flags()&flagFIN) == 0 || (out.flags()&flagACK) == 0 || (out.flags()&flagSN) != 0 ||
		(out.flags()&flagAN) != 0 {
		t.Error()
	}
	p = makePacketWithoutData(flagFIN|flagAN, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorH2(p) }()
	sendEvt = <-c.sendRoutineEvent
	out = <-c.outgoingPackets
	newState = <-c.setState
	lock.Lock()
	if needContinue || err != nil || sendEvt != discardPacket || newState != stateLastAck ||
		(out.flags()&flagFIN) == 0 || (out.flags()&flagACK) == 0 || (out.flags()&flagSN) == 0 ||
		(out.flags()&flagAN) == 0 {
		t.Error()
	}
	p = makePacketWithoutData(flagFIN|flagSN|flagAN, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorH2(p) }()
	sendEvt = <-c.sendRoutineEvent
	out = <-c.outgoingPackets
	newState = <-c.setState
	lock.Lock()
	if needContinue || err != nil || sendEvt != discardPacket || newState != stateLastAck ||
		(out.flags()&flagFIN) == 0 || (out.flags()&flagACK) == 0 || (out.flags()&flagSN) == 0 ||
		(out.flags()&flagAN) != 0 {
		t.Error()
	}

	// If any data remains to be sent then
	// inform the user "Warning: Data left unsent."
	// Violation! Treat data left unsent as error.
	c.txBuf = []byte{0x42}
	p = makePacketWithoutData(flagFIN, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorH2(p) }()
	sendEvt = <-c.sendRoutineEvent
	newState = <-c.setState
	lock.Lock()
	if needContinue || err != ErrDataLeftUnsent || sendEvt != discardPacket ||
		newState != stateClosed {
		t.Error()
	}
}

func TestBehaviorH3(t *testing.T) {
	// Behavior H3 not implemented
	defer func() {
		if r := recover(); r == nil {
			t.Error()
		}
	}()

	c := createMockConnection(256)
	c.behaviorH3(packet{})
}

func TestBehaviorH4(t *testing.T) {
	c := createMockConnection(256)
	var p packet
	var needContinue bool
	var err error
	var lock sync.Mutex
	lock.Lock()

	// If the AN field value is expected then this ACK is in response
	// to the FIN, ACK packet recently sent.  This is the final
	// acknowledging message indicating both side's agreement to close
	// the connection.  Discard the packet, flush all queues, delete
	// the TCB, and go to the CLOSED state without any further
	// processing.
	p = makePacketWithoutData(flagAN, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorH4(p) }()
	sendEvt := <-c.sendRoutineEvent
	newState := <-c.setState
	lock.Lock()
	if needContinue || err != nil || sendEvt != discardPacket ||
		newState != stateClosed {
		t.Error()
	}

	// Otherwise the AN field value was unexpected.  Discard the
	// packet and remain in the current state without any further
	// processing.
	p = makePacketWithoutData(0, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorH4(p) }()
	lock.Lock()
	if needContinue || err != nil {
		t.Error()
	}
}

func TestBehaviorH5(t *testing.T) {
	// Behavior H5 not implemented
	defer func() {
		if r := recover(); r == nil {
			t.Error()
		}
	}()

	c := createMockConnection(256)
	c.behaviorH5(packet{})
}

func TestBehaviorH6(t *testing.T) {
	// Behavior H6 not implemented
	defer func() {
		if r := recover(); r == nil {
			t.Error()
		}
	}()

	c := createMockConnection(256)
	c.behaviorH6(packet{})
}

func TestBehaviorI1(t *testing.T) {
	c := createMockConnection(256)
	var p packet
	var needContinue bool
	var err error
	var lock sync.Mutex
	lock.Lock()

	// The packet is examined to see if it contains
	// data.  If not the packet is now discarded, return to the
	// current state without any further processing.
	p = makePacketWithoutData(0, 0)
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorI1(p) }()
	lock.Lock()
	if needContinue || err != nil {
		t.Error()
	}

	// We assume the packet contained data, that either the SO flag
	// was set or LENGTH is positive. An acknowledgment is sent:
	//    <SN=received AN><AN=received SN+1 modulo 2><CTL=ACK>
	c.recvSN = 1
	c.rxBuf = nil
	p, err = makePacketWithData(flagSN, []byte{0x42})
	if err != nil {
		t.Error()
	}
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorI1(p) }()
	out := <-c.outgoingPackets
	lock.Lock()
	if needContinue || err != nil || (out.flags()&flagACK) == 0 || (out.flags()&flagSN) != 0 ||
		(out.flags()&flagAN) != 0 {
		t.Error()
	}
	if len(c.rxBuf) != 1 || c.rxBuf[0] != 0x42 {
		t.Error()
	}
	c.recvSN = 1
	c.rxBuf = nil
	p, err = makePacketWithData(flagSN|flagAN, []byte{0x42})
	if err != nil {
		t.Error()
	}
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorI1(p) }()
	out = <-c.outgoingPackets
	lock.Lock()
	if needContinue || err != nil || (out.flags()&flagACK) == 0 || (out.flags()&flagSN) == 0 ||
		(out.flags()&flagAN) != 0 {
		t.Error()
	}
	if len(c.rxBuf) != 1 || c.rxBuf[0] != 0x42 {
		t.Error()
	}
	c.recvSN = 0
	c.rxBuf = nil
	p, err = makePacketWithData(0, []byte{0x42})
	if err != nil {
		t.Error()
	}
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorI1(p) }()
	out = <-c.outgoingPackets
	lock.Lock()
	if needContinue || err != nil || (out.flags()&flagACK) == 0 || (out.flags()&flagSN) != 0 ||
		(out.flags()&flagAN) == 0 {
		t.Error()
	}
	if len(c.rxBuf) != 1 || c.rxBuf[0] != 0x42 {
		t.Error()
	}
	c.recvSN = 0
	c.rxBuf = nil
	p, err = makePacketWithData(flagAN, []byte{0x42})
	if err != nil {
		t.Error()
	}
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorI1(p) }()
	out = <-c.outgoingPackets
	lock.Lock()
	if needContinue || err != nil || (out.flags()&flagACK) == 0 || (out.flags()&flagSN) == 0 ||
		(out.flags()&flagAN) == 0 {
		t.Error()
	}
	if len(c.rxBuf) != 1 || c.rxBuf[0] != 0x42 {
		t.Error()
	}
	c.recvSN = 1
	c.rxBuf = nil
	p, err = makePacketWithData(flagSN, []byte{0x11, 0x22})
	if err != nil {
		t.Error()
	}
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorI1(p) }()
	out = <-c.outgoingPackets
	lock.Lock()
	if needContinue || err != nil || (out.flags()&flagACK) == 0 || (out.flags()&flagSN) != 0 ||
		(out.flags()&flagAN) != 0 {
		t.Error()
	}
	if len(c.rxBuf) != 2 || c.rxBuf[0] != 0x11 || c.rxBuf[1] != 0x22 {
		t.Error()
	}
	c.recvSN = 1
	c.rxBuf = nil
	p, err = makePacketWithData(flagSN|flagAN, []byte{0x11, 0x22})
	if err != nil {
		t.Error()
	}
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorI1(p) }()
	out = <-c.outgoingPackets
	lock.Lock()
	if needContinue || err != nil || (out.flags()&flagACK) == 0 || (out.flags()&flagSN) == 0 ||
		(out.flags()&flagAN) != 0 {
		t.Error()
	}
	if len(c.rxBuf) != 2 || c.rxBuf[0] != 0x11 || c.rxBuf[1] != 0x22 {
		t.Error()
	}
	c.recvSN = 0
	c.rxBuf = nil
	p, err = makePacketWithData(0, []byte{0x11, 0x22})
	if err != nil {
		t.Error()
	}
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorI1(p) }()
	out = <-c.outgoingPackets
	lock.Lock()
	if needContinue || err != nil || (out.flags()&flagACK) == 0 || (out.flags()&flagSN) != 0 ||
		(out.flags()&flagAN) == 0 {
		t.Error()
	}
	if len(c.rxBuf) != 2 || c.rxBuf[0] != 0x11 || c.rxBuf[1] != 0x22 {
		t.Error()
	}
	c.recvSN = 0
	c.rxBuf = nil
	p, err = makePacketWithData(flagAN, []byte{0x11, 0x22})
	if err != nil {
		t.Error()
	}
	go func() { defer lock.Unlock(); needContinue, err = c.behaviorI1(p) }()
	out = <-c.outgoingPackets
	lock.Lock()
	if needContinue || err != nil || (out.flags()&flagACK) == 0 || (out.flags()&flagSN) == 0 ||
		(out.flags()&flagAN) == 0 {
		t.Error()
	}
	if len(c.rxBuf) != 2 || c.rxBuf[0] != 0x11 || c.rxBuf[1] != 0x22 {
		t.Error()
	}
}
