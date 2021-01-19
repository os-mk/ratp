package ratp

import (
	"errors"
	"io"
	"sync"
	"time"
)

// ErrNotImpl is the error returned if the requested function or behavior is not implemented.
var ErrNotImpl error = errors.New("Not implemented")

// ErrConnRefused is the error returned when a peer refuses an incoming connection request.
var ErrConnRefused error = errors.New("Connection refused")

// ErrConnReset is the error returned when an established connection was terminated by a peer.
var ErrConnReset error = errors.New("Connection reset")

// ErrRetransmission is the error returned when an established connection was reset due to exceeding
// the retransmission limit.
var ErrRetransmission error = errors.New("Connection aborted due to retransmission failure")

// ErrDataLeftUnsent is the error returned if an established connection was terminated by a peer and
// unsent data left in the outgoing buffer.
var ErrDataLeftUnsent error = errors.New("Data left unsent")

// ErrInvalMDL is the error returned by Connect if incorrect maximum data length (MDL) passed. Valid
// range: 1 ≤ MDL ≤ 255.
var ErrInvalMDL error = errors.New("1 ≤ MDL ≤ 255")

// Logger is an interface used to pass abstract logger implementation to the package.
type Logger interface {
	Print(...interface{})
	Printf(string, ...interface{})
	Println(...interface{})
}

// RATP is the class, implementing RATP communications.
type RATP struct {
	log Logger
}

// Connection is the class, implementing io.ReadWriteCloser operations over RATP communications.
type Connection struct {
	// Common data
	port  io.ReadWriteCloser
	wg    sync.WaitGroup
	state int
	mdl   int
	log   Logger

	// stateRoutine data
	getState                chan int
	setState                chan int
	getMDL                  chan int
	setMDL                  chan int
	stateRoutineTermination chan struct{}

	// sendRoutine data
	outgoingPackets        chan packet
	sendRoutineTermination chan struct{}
	sendRoutineEvent       chan routineEvent
	sendRoutineError       chan error
	rto                    time.Duration

	// recvRoutine data
	recvRoutineTermination chan struct{}
	recvRoutineError       chan error
	recvConnEstablished    chan struct{}
	recvTxData             chan []byte
	recvRxData             chan []byte
	recvBytesRead          chan int
	recvSN                 int
	sendSN                 int
	txBuf                  []byte
	rxBuf                  []byte
	emptyTx                bool
}

// New creates new instanse of RATP class. Parameter logger is an optional (can be nil) and used
// for passing an implementation of a logger for debugging purposes.
func New(logger Logger) *RATP {
	var ratp RATP
	if logger != nil {
		ratp.log = logger
	} else {
		ratp.log = nilLogger{}
	}
	return &ratp
}

// Connect creates a Connection object and performs a RATP active open over the serial device
// identified by a string passed in serial parameter. Parameter serialCfg determines the
// configuration of this device (e.g. "115200,8E1" for 115200 baud/s, 8 data bits, 1 stop bit,
// parity check enabled and set to "Even"). mdl (stands for "Maximum Data Length") determines the
// maximum size of RATP packet. Should be in the range 1 ≤ MDL ≤ 255. Use 255 if unsure.
func (r *RATP) Connect(serial string, serialCfg string, mdl int) (*Connection, error) {
	if mdl < 1 || mdl > 255 {
		return nil, ErrInvalMDL
	}

	conn := createFilledConnection()

	conn.mdl = mdl
	conn.log = r.log

	var err error
	conn.port, err = openPort(serial, serialCfg, readTimeout)
	if err != nil {
		return nil, err
	}

	startRoutines(conn)

	return establishConnection(conn)
}

// Read reads up to len(p) bytes into p. It returns the number of bytes read (0 <= n <= len(p))
// and any error encountered. If some data is available but not len(p) bytes, Read conventionally
// returns what is available instead of waiting for more.
func (c *Connection) Read(data []byte) (int, error) {
	state := <-c.getState

	if state != stateEstablished && state != stateAltEstablished {
		return 0, io.EOF
	}

	if state == stateAltEstablished {
		// Send deferred acknowledgment for SYN+ACK
		p := makePacketWithoutData(flagACK|flagSN|flagAN, 0)
		c.outgoingPackets <- p
		c.setState <- stateEstablished
	}

	var n int

	select {
	case rxBuf := <-c.recvRxData:
		n = copy(data, rxBuf)

	case err := <-c.recvRoutineError:
		return 0, err

	case err := <-c.sendRoutineError:
		return 0, err
	}

	if n == 0 {
		return 0, io.EOF
	}

	select {
	case c.recvBytesRead <- n:

	case err := <-c.recvRoutineError:
		return 0, err

	case err := <-c.sendRoutineError:
		return 0, err
	}

	return n, nil
}

// Write writes len(p) bytes from p to the RATP connection. It returns the number of bytes written
// from p (0 <= n <= len(p)) and any error encountered that caused the write to stop early. Write
// returns a non-nil error if it returns n < len(p).
func (c *Connection) Write(p []byte) (int, error) {
	state := <-c.getState
	if state != stateEstablished && state != stateAltEstablished {
		return 0, io.EOF
	}
	if state == stateAltEstablished {
		c.setState <- stateEstablished
	}
	c.recvTxData <- p
	return len(p), nil
}

// Close closes the connection. The behavior of all methods after the first call of Close is
// undefined.
func (c *Connection) Close() error {
	close(c.stateRoutineTermination)
	close(c.sendRoutineTermination)
	close(c.recvRoutineTermination)

	// Gather errors
	var err error
loop:
	for {
		select {
		case err = <-c.sendRoutineError:
		case err = <-c.recvRoutineError:
		default:
			break loop
		}
	}

	c.wg.Wait()

	portErr := c.port.Close()
	if err == nil {
		return portErr
	}
	return err
}

var readTimeout time.Duration = 100 * time.Millisecond

const maxRetries = 5

const (
	stateListen = iota
	stateSynSent
	stateSynReceived
	stateAltEstablished // After SYN+ACK received
	stateEstablished
	stateFinWait
	stateLastAck
	stateClosing
	stateTimeWait
	stateClosed
)

func stateToStr(state int) string {
	switch state {
	case stateListen:
		return "LISTEN"
	case stateSynSent:
		return "SYN-SENT"
	case stateAltEstablished, stateEstablished:
		return "ESTABLISHED"
	case stateFinWait:
		return "FIN-WAIT"
	case stateLastAck:
		return "LAST-ACK"
	case stateClosing:
		return "CLOSING"
	case stateTimeWait:
		return "TIME-WAIT"
	case stateClosed:
		return "CLOSED"
	default:
		return "UNKNOWN"
	}
}

const (
	discardPacket = iota
	restartRetransmission
)

type routineEvent int

type recvDataReq struct {
	size int
	out  chan<- []byte
}

func createFilledConnection() *Connection {
	var conn Connection

	conn.state = stateClosed
	conn.getState = make(chan int)
	conn.setState = make(chan int)
	conn.getMDL = make(chan int)
	conn.setMDL = make(chan int)
	conn.stateRoutineTermination = make(chan struct{}, 1)

	conn.sendRoutineTermination = make(chan struct{}, 1)
	conn.outgoingPackets = make(chan packet)
	conn.sendRoutineEvent = make(chan routineEvent)
	conn.sendRoutineError = make(chan error)
	conn.rto = 350 * time.Millisecond

	conn.recvRoutineTermination = make(chan struct{}, 1)
	conn.recvConnEstablished = make(chan struct{})
	conn.recvRoutineError = make(chan error)
	conn.recvTxData = make(chan []byte)
	conn.recvRxData = make(chan []byte)
	conn.recvBytesRead = make(chan int)
	conn.emptyTx = true
	conn.recvSN = 0
	conn.sendSN = 1 // Next packet SN. First packet is always 0.

	return &conn
}

func startRoutines(conn *Connection) {
	conn.wg.Add(1)
	go conn.stateRoutine()

	conn.wg.Add(1)
	go conn.recvRoutine()

	conn.wg.Add(1)
	go conn.sendRoutine()
}

func establishConnection(conn *Connection) (*Connection, error) {
	conn.setState <- stateSynSent
	syn := makePacketWithoutData(flagSYN, byte(conn.mdl))
	conn.outgoingPackets <- syn

	conn.log.Print("SYN sent, waiting for acknowledgment")

	select {
	case err := <-conn.recvRoutineError:
		return nil, err

	case err := <-conn.sendRoutineError:
		return nil, err

	case <-conn.recvConnEstablished:
		conn.log.Print("Connection established")
	}

	return conn, nil
}

func (c *Connection) stateRoutine() {
	defer c.wg.Done()
	defer close(c.getState)
	defer close(c.getMDL)

	for {
		select {
		case c.getState <- c.state:

		case newState := <-c.setState:
			c.state = newState

		case c.getMDL <- c.mdl:

		case newMDL := <-c.setMDL:
			c.mdl = newMDL

		case <-c.stateRoutineTermination:
			return
		}
	}
}

func (c *Connection) sendRoutine() {
	defer c.wg.Done()

	var currPacket packet
	var retryCount int = 0

	var ticker *time.Ticker
	var tickerC <-chan time.Time
	defer func() {
		if ticker != nil {
			ticker.Stop()
		}
	}()

	for {
		select {
		case p := <-c.outgoingPackets:
			if isACKRequired(p.flags()) {
				currPacket = p
				retryCount = 0
			} else {
				_, err := c.port.Write([]byte(p))
				c.log.Printf("[out] %v\n", &p)
				if err != nil {
					c.sendRoutineError <- err
					return
				}
				continue
			}

		case <-tickerC:
			retryCount++
			if retryCount >= maxRetries {
				c.sendRoutineError <- ErrRetransmission
				return
			}

		case e := <-c.sendRoutineEvent:
			switch e {
			case discardPacket:
				currPacket = nil
				if ticker != nil {
					ticker.Stop()
					ticker = nil
				}

			case restartRetransmission:
				retryCount = 0
			}
			continue

		case <-c.sendRoutineTermination:
			return
		}

		if ticker != nil {
			ticker.Stop()
			ticker = nil
			tickerC = nil
		}

		_, err := c.port.Write([]byte(currPacket))
		if retryCount == 0 {
			c.log.Printf("[out] %v\n", &currPacket)
		} else {
			c.log.Printf("[out] (try %d of %d) %v\n", retryCount+1, maxRetries, &currPacket)
		}
		if err != nil {
			c.sendRoutineError <- err
			return
		}

		ticker = time.NewTicker(c.rto)
		tickerC = ticker.C
	}
}

func (c *Connection) recvRoutine() {
	defer c.wg.Done()
	defer close(c.recvRxData)

	dataRead := make(chan []byte)
	closing := make(chan struct{})
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		var buf [1]byte
		for {
			n, err := c.port.Read(buf[:])
			if err != nil && err != io.EOF {
				c.recvRoutineError <- err
				break
			}
			if n > 0 {
				var cp [1]byte
				cp[0] = buf[0]
				select {
				case dataRead <- cp[:]:
					continue

				case <-closing:
					return
				}
			}
			select {
			case <-closing:
				return
			default:
			}
		}
	}()

	defer close(closing)

	dec := makePacketDecoder()

	for {
		var optRxBufChan chan []byte
		if len(c.rxBuf) > 0 {
			optRxBufChan = c.recvRxData
		}

		select {
		case <-c.recvRoutineTermination:
			return

		case data := <-c.recvTxData:
			c.txBuf = append(c.txBuf, data...)
			if c.emptyTx {
				c.emptyTx = false
				c.sendNextData()
			}

		case data := <-dataRead:
			p, n := dec(data[:])
			if n != len(data) {
				panic("Couldn't handle data")
			}
			if p != nil {
				err := c.handlePacket(p)
				if err != nil {
					c.recvRoutineError <- err
					return
				}
				if <-c.getState == stateClosed {
					return
				}
			}

		case optRxBufChan <- c.rxBuf:

		case n := <-c.recvBytesRead:
			c.rxBuf = c.rxBuf[n:] // TODO: use another queue implementation to prevent leak
		}
	}
}

func (c *Connection) handlePacket(p packet) error {
	c.log.Printf("[in]  %v\n", &p)

	behaviors := [...][]func(packet) (bool, error){
		stateListen:      {c.behaviorA},
		stateSynSent:     {c.behaviorB},
		stateSynReceived: {c.behaviorC1, c.behaviorD1, c.behaviorE, c.behaviorF1, c.behaviorH1},
		stateAltEstablished: {c.behaviorC2, c.behaviorD2, c.behaviorE, c.behaviorF2,
			c.behaviorH2, c.behaviorI1},
		stateEstablished: {c.behaviorC2, c.behaviorD2, c.behaviorE, c.behaviorF2, c.behaviorH2,
			c.behaviorI1},
		stateFinWait:  {c.behaviorC2, c.behaviorD2, c.behaviorE, c.behaviorF3, c.behaviorH3},
		stateLastAck:  {c.behaviorC2, c.behaviorD3, c.behaviorE, c.behaviorF3, c.behaviorH4},
		stateClosing:  {c.behaviorC2, c.behaviorD3, c.behaviorE, c.behaviorF3, c.behaviorH5},
		stateTimeWait: {c.behaviorD3, c.behaviorE, c.behaviorF3, c.behaviorH6},
		stateClosed:   {c.behaviorG}}

	currState := <-c.getState
	if currState >= len(behaviors) {
		panic("Impossible state")
	}

	c.log.Printf("Current state: %s", stateToStr(currState))

	behs := behaviors[currState]
	for _, b := range behs {
		cont, err := b(p)
		if err != nil {
			return err
		}
		if !cont {
			break
		}
	}
	return nil
}

func anToSN(p packet) flags {
	var flags flags
	if (p.flags() & flagAN) != 0 {
		flags |= flagSN
	}
	return flags
}

func snToAN(p packet) flags {
	var flags flags
	if (p.flags() & flagSN) == 0 {
		flags |= flagAN
	}
	return flags
}

func (c *Connection) isANExpected(p packet) bool {
	an := (p.flags() & flagAN) != 0
	ctr := (c.sendSN & 1) != 0
	return an == ctr
}

func isACKRequired(f flags) bool {
	if (f & flagSO) != 0 {
		return true
	}
	if (f & (flagSYN | flagRST | flagFIN)) != 0 {
		return true
	}
	return false
}

func (c *Connection) isSNExpected(p packet) bool {
	if !isACKRequired(p.flags()) {
		return true
	}
	sn := (p.flags() & flagSN) != 0
	ctr := (c.recvSN & 1) != 0
	return sn == ctr
}

func (c *Connection) sendNextData() {
	if len(c.txBuf) == 0 {
		c.emptyTx = true
		c.log.Print("Nothing to send")
		return
	}
	toSend := <-c.getMDL
	if len(c.txBuf) < toSend {
		toSend = len(c.txBuf)
	}

	var flags flags = flagACK
	if (c.sendSN & 1) != 0 {
		flags |= flagSN
	}
	if (c.recvSN & 1) != 0 {
		flags |= flagAN
	}
	// TODO: use another queue implementation to prevent leak
	p, _ := makePacketWithData(flags, c.txBuf[:toSend]) // impossible to get an error here
	c.sendSN++
	c.txBuf = c.txBuf[toSend:]
	c.outgoingPackets <- p
}

func (c *Connection) behaviorA(p packet) (bool, error) {
	c.log.Println("LISTEN state not implemented")
	var flags flags = flagRST | anToSN(p)
	rsp := makePacketWithoutData(flags, 0)
	c.outgoingPackets <- rsp
	return false, ErrNotImpl
}

func (c *Connection) behaviorB(p packet) (bool, error) {
	c.log.Print("Behavior B invoked")

	if (p.flags() & flagACK) != 0 {
		if !c.isANExpected(p) {
			c.log.Print("Unexpected AN")
			if (p.flags() & flagRST) == 0 {
				rsp := makePacketWithoutData(flagRST|anToSN(p), 0)
				c.outgoingPackets <- rsp
			}
			return false, nil
		}
	}

	if (p.flags() & flagRST) != 0 {
		if (p.flags() & flagACK) != 0 {
			c.sendRoutineEvent <- discardPacket
			c.setState <- stateClosed
			return false, ErrConnRefused
		}
		return false, nil
	}

	if (p.flags() & flagSYN) != 0 {
		if (p.flags() & flagACK) != 0 {
			c.setMDL <- int(p[posLength])
			c.sendSN = 0
			if (p.flags() & flagAN) != 0 {
				c.sendSN = 1
			}
			c.recvSN = 1
			if (p.flags() & flagSN) != 0 {
				c.recvSN = 0
			}
			c.sendRoutineEvent <- discardPacket
			c.setState <- stateAltEstablished
			c.recvConnEstablished <- struct{}{}
		} else {
			c.log.Println("Recovering from a Simultaneous Active OPEN not supported")
			c.sendRoutineEvent <- discardPacket
			c.setState <- stateClosed
			return false, ErrNotImpl
		}
	}

	return false, nil
}

func (c *Connection) behaviorC1(p packet) (bool, error) {
	c.log.Println("SYN-RECEIVED state not implemented")
	rsp := makePacketWithoutData(flagRST|anToSN(p), 0)
	c.outgoingPackets <- rsp
	return false, ErrNotImpl
}

func (c *Connection) behaviorC2(p packet) (bool, error) {
	c.log.Print("Behavior C2 invoked")

	if c.isSNExpected(p) {
		return true, nil
	}

	if (p.flags() & (flagRST | flagFIN)) != 0 {
		return false, nil
	}

	if (p.flags() & flagSYN) != 0 {
		rsp := makePacketWithoutData(flagRST|flagACK|anToSN(p)|snToAN(p), 0)
		c.sendRoutineEvent <- discardPacket
		c.outgoingPackets <- rsp
		c.setState <- stateClosed
		return false, ErrConnReset
	}

	// This packet is a duplicate of one already received. Send an ACK back.
	rsp := makePacketWithoutData(flagACK|anToSN(p)|snToAN(p), 0)
	c.outgoingPackets <- rsp

	return false, nil
}

func (c *Connection) behaviorD1(p packet) (bool, error) {
	panic("Not implemented")
}

func (c *Connection) behaviorD2(p packet) (bool, error) {
	c.log.Print("Behavior D2 invoked")

	if (p.flags() & flagRST) == 0 {
		return true, nil
	}
	c.sendRoutineEvent <- discardPacket
	c.setState <- stateClosed
	return false, ErrConnReset
}

func (c *Connection) behaviorD3(p packet) (bool, error) {
	c.log.Print("Behavior D3 invoked")

	if (p.flags() & flagRST) == 0 {
		return true, nil
	}
	c.sendRoutineEvent <- discardPacket
	c.setState <- stateClosed
	return false, ErrConnReset
}

func (c *Connection) behaviorE(p packet) (bool, error) {
	c.log.Print("Behavior E invoked")

	if (p.flags() & flagSYN) == 0 {
		return true, nil
	}
	var flags flags = flagRST
	if (p.flags() & flagACK) != 0 {
		flags |= anToSN(p)
	}
	rsp := makePacketWithoutData(flags, 0)
	c.sendRoutineEvent <- discardPacket
	c.outgoingPackets <- rsp
	c.setState <- stateClosed
	return false, ErrConnReset
}

func (c *Connection) behaviorF1(p packet) (bool, error) {
	panic("Not implemented")
}

func (c *Connection) behaviorF2(p packet) (bool, error) {
	c.log.Print("Behavior F2 invoked")

	if (p.flags() & flagACK) == 0 {
		return false, nil
	}

	if !c.isANExpected(p) {
		// Duplicate acknowledgment treated as "need to wait" mark.
		c.log.Print("Duplicated acknowledgment")
		c.sendRoutineEvent <- restartRetransmission
		return false, nil
	}

	c.log.Print("Data acknowledged. Ready to send next.")

	c.sendNextData()

	return true, nil
}

func (c *Connection) behaviorF3(p packet) (bool, error) {
	c.log.Print("Behavior F3 invoked")

	return (p.flags() & flagACK) != 0, nil
}

func (c *Connection) behaviorG(p packet) (bool, error) {
	if (p.flags() & flagRST) == 0 {
		if (p.flags() & flagACK) != 0 {
			rsp := makePacketWithoutData(flagRST|anToSN(p), 0)
			c.outgoingPackets <- rsp
		} else {
			rsp := makePacketWithoutData(flagRST|flagACK|snToAN(p), 0)
			c.outgoingPackets <- rsp
		}
	}
	return false, nil
}

func (c *Connection) behaviorH1(p packet) (bool, error) {
	panic("Not implemented")
}

func (c *Connection) behaviorH2(p packet) (bool, error) {
	c.log.Print("Behavior H2 invoked")

	if (p.flags() & flagFIN) == 0 {
		return true, nil
	}

	if len(c.txBuf) > 0 {
		c.sendRoutineEvent <- discardPacket
		c.setState <- stateClosed
		return false, ErrDataLeftUnsent
	}

	c.sendSN++
	rsp := makePacketWithoutData(flagFIN|flagACK|anToSN(p)|snToAN(p), 0)
	c.sendRoutineEvent <- discardPacket
	c.outgoingPackets <- rsp

	c.setState <- stateLastAck

	return false, nil
}

func (c *Connection) behaviorH3(p packet) (bool, error) {
	panic("Not implemented")
}

func (c *Connection) behaviorH4(p packet) (bool, error) {
	c.log.Print("Behavior H4 invoked")

	if !c.isANExpected(p) {
		c.log.Print("Unexpected AN")
		return false, nil
	}

	c.sendRoutineEvent <- discardPacket
	c.setState <- stateClosed
	return false, nil
}

func (c *Connection) behaviorH5(p packet) (bool, error) {
	panic("Not implemented")
}

func (c *Connection) behaviorH6(p packet) (bool, error) {
	panic("Not implemented")
}

func (c *Connection) behaviorI1(p packet) (bool, error) {
	c.log.Print("Behavior I1 invoked")

	if (p.flags() & flagSO) != 0 {
		c.rxBuf = append(c.rxBuf, p[posLength])
	} else if p[posLength] > 0 {
		c.rxBuf = append(c.rxBuf, p[posData:posData+int(p[posLength])]...)
	} else {
		return false, nil
	}
	c.recvSN++
	var flags flags = flagACK | anToSN(p)
	if (c.recvSN & 1) != 0 {
		flags |= flagAN
	}
	rsp := makePacketWithoutData(flags, 0)
	c.outgoingPackets <- rsp
	return false, nil
}
