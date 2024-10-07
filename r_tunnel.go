package main
import (
	"net"
	"time"
	"sync"
	"encoding/binary"
	"github.com/pkg/errors"
)

//notes:
// for extending the protocol, perhaps use https://github.com/raszia/gotiny serialization?
// supports encryption too....

var g_udp_overhead=20+8 // (IP = 20, udp =8)

var g_listener *net.UDPConn
var g_accept_chan chan *RatSession

var g_remoteConns *sync.Map

type RatSession struct {
	convid uint32

	udp_conn    *net.UDPConn // the underlying packet connection	
	local_addr  *net.UDPAddr // locally bound address     
	remote_addr *net.UDPAddr // remote peer address
	is_client bool
	is_closed bool

	rd         	time.Time // read deadline
	mu 			sync.Mutex

	chReadEvent  chan []byte
	chCloseEvent  chan []byte
	chSocketReadError    chan struct{}

	mtu int
}

var (
	ErrConnectionectionOpen   = errors.New("Connectionection is already open.")
	ErrConnectionectionClosed = errors.New("Connectionection is already closed.")
	ErrPacketSize             = errors.New("Packet size exceeds MTU.")
	ErrShortWrite             = errors.New("Short write: Send was incomplete.")
	ErrDiscard                = errors.New("Packet is not meant for us.")
	ErrTimeout                = errors.New("timeout")
)


// NewClientSession establishes a session and talks RATBOND protocol over a packet connection.
func NewClientSession(convid uint32, localaddr *net.UDPAddr,remoteaddr *net.UDPAddr, conn *net.UDPConn) (*RatSession, error) {
	if (convid==0) {
		l.Panicf("convid = 0 !")
	}
	session:=new(RatSession)
	session.convid=convid
	
	session.remote_addr=remoteaddr
	session.local_addr=localaddr
	session.udp_conn=conn
	session.is_client=true
	session.is_closed=false
	session.mtu=1300

	l.Tracef("NewRatSession: %+v",session)
	
	return session,nil
}

func NewServerSession(convid uint32, localaddr *net.UDPAddr,remoteaddr *net.UDPAddr, conn *net.UDPConn) (*RatSession, error) {
	if (convid==0) {
		l.Panicf("convid = 0 !")
	}
	session:=new(RatSession)
	session.convid=convid
	
	session.remote_addr=remoteaddr
	session.local_addr=localaddr
	session.udp_conn=conn
	session.is_client=false
	session.is_closed=false
	session.mtu=1300

	session.chReadEvent = make(chan []byte, 1)
	session.chCloseEvent = make(chan []byte, 1)
	session.chSocketReadError = make(chan struct{})

	l.Tracef("NewRatSession: %+v",session)
	
	return session,nil
}

// Read implements net.Conn
func (s *RatSession) Read(b []byte) (n int, err error) {
	if s.is_closed {
		return 0,errors.New("session is closed")
	}
	if s==nil {
		return 0,errors.New("nil session")
	}

	//client connections are simple, simply use the upper layer read.
	if (s.is_client) {
		return s.udp_conn.Read(b)
	}


	//server connections are harder, we have to wait for a message from the channel, or pass the deadline
//RESET_TIMER:
    var timeout *time.Timer
    // deadline for current reading operation
    var c <-chan time.Time
    if !s.rd.IsZero() {
        delay := time.Until(s.rd)
        timeout = time.NewTimer(delay)
        c = timeout.C
        defer timeout.Stop()
    }	

	 // if it runs here, that means we have to block the call, and wait until the
    // next data packet arrives via the channel
	select {
		case buff := <-s.chReadEvent:
			/*if timeout != nil {
				timeout.Stop()
				goto RESET_TIMER
			}
			*/
			l.Tracef("Read got data len:%d data:%x", len(buff),buff)
			//TODO: this involves copying the buffer, not great
			copy(b,buff)
			return len(buff),nil
		case <-s.chCloseEvent:
			l.Errorf("CONNECTION WAS CLOSED: %s",s.remote_addr.String())
			return 0,errors.New("connection closed")
		case <-c:
			return 0, errors.WithStack(ErrTimeout)
	}
}

func (s *RatSession) DataReceived(b []byte) { 
	l.Tracef("DataReceived: len:%d data:%x, remote:%s",len(b),b,s.remote_addr.String())
	if (s.is_closed) {
		return
	}
	s.chReadEvent <- b
}

// Write implements net.Conn
func (s *RatSession) Write(b []byte) (n int, err error) { 
	l.Tracef("session write convid:%d, len:%d, dst:%+v, buf:%+v",s.convid,len(b),s.remote_addr,b)
	if s.is_closed {
		return 0,errors.New("session is closed")
	}

	if s==nil {
		return 0,errors.New("nil session")
	}
	if len(b) > s.PayloadSize() {
		return 0,ErrPacketSize
	}
	
	if (s.is_client) {
		return s.udp_conn.Write(b)
	} else {
		return s.udp_conn.WriteToUDP(b,s.remote_addr)
	}
}

func (s *RatSession) Close() error {
	if s==nil {
		l.Errorf("session is nil!")
		return errors.New("nil session")
	}
	
	s.is_closed=true
	l.Warnf("closing connection to %s",s.remote_addr.String())
	if (s.is_client) {
		return s.udp_conn.Close()
	} else {
		g_remoteConns.Delete(s.remote_addr.String())

		g_remoteConns.Range(func(key, value interface{}) bool {
			l.Infof("remoteConn:%s",key.(string))
			return true
		})

		//send an empty array to notify the reader of the close event
		b:=make([]byte, 0)
		s.chCloseEvent <- b

		return nil
	}
	
}
	
func (s *RatSession) GetState() uint32 { 
	if s==nil {
		return 0
	}
	return 0
}


func (s *RatSession) PayloadSize() int {
	size := int(s.mtu) - g_udp_overhead
	return size
}

func (s *RatSession) GetMss() int {
	return s.PayloadSize()
}

func (s *RatSession) SetMss(mss int) {
	s.SetMtu(mss+g_udp_overhead)
}


func (s *RatSession) SetMtu(mtu int) {
	s.mtu=mtu
}

func (s *RatSession) GetMtu() int {
	return s.mtu
}

func (s *RatSession) SetReadBuffer(bytes int) error {
	if s==nil {
		return errors.New("nil session")
	}
	return nil
}

func (s *RatSession) SetWriteBuffer(bytes int) error {
	if s==nil {
		return errors.New("nil session")
	}
	return nil
}


// SetReadDeadline implements the Conn SetReadDeadline method.
func (s *RatSession) SetReadDeadline(t time.Time) error {
	if s==nil {
		return errors.New("nil session")
	}
	if (s.is_client) {
		return s.udp_conn.SetReadDeadline(t)
	}

	//server implementation
	s.mu.Lock()
    s.rd = t
    s.mu.Unlock()
    return nil
}

// SetWriteDeadline implements the Conn SetWriteDeadline method.
func (s *RatSession) SetWriteDeadline(t time.Time) error {
	if s==nil {
		return errors.New("nil session")
	}
	return s.udp_conn.SetWriteDeadline(t)
}

// RemoteAddr returns the remote network address.
func (s *RatSession) RemoteAddr() net.Addr { 
	if s!=nil {
		return s.remote_addr
	} 
	return nil
}

// LocalAddr returns the local network address.
func (s *RatSession) LocalAddr() net.Addr { 
	if s!=nil {
		return s.remote_addr
	} 
	return nil
}

// GetConv gets conversation id of a session
func (s *RatSession) GetConv() uint32 { 
	if s!=nil {
		return s.convid 
	}
	return 0
}


// Send a hello message over the session
func (s *RatSession) SendHello() (bool, error) { 
	if (s.is_closed) {
		return false,errors.New("session is closed")
	}

	l.Debugf("sending hello to %s",s.remote_addr.String())
	//Send a hello message with our tunnel id
	s.SetWriteDeadline(time.Now().Add(time.Millisecond*g_write_deadline))
	var pkt []byte
	if s.is_client {
		pkt=make([]byte, len(g_client_hello))
		copy(pkt,g_client_hello)
	} else {
		pkt=make([]byte, len(g_client_hello))
		copy(pkt,g_server_hello)
	}
	b := make([]byte, 2)
	binary.LittleEndian.PutUint16(b, uint16(s.convid))
	pkt[2]=b[0]
	pkt[3]=b[1]

	//TODO: we may want to add a sequence number

	n,err:=s.Write(pkt)
	if err!=nil || n==0 {
		l.Errorf("send hello FAILED! err:%s n:%d",err,n )
		return false,err
	}

	return true,nil
}

// ========================== LISTENER ===========================
// ================================================================
func createListener(listen_addr string) (*net.UDPConn, error) {
	l.Debugf("createListener")
	l.Infof("listening on %s",listen_addr)
	udpaddr, err := net.ResolveUDPAddr("udp", listen_addr)
    if err != nil {
        return nil, errors.WithStack(err)
    }
    conn, err := net.ListenUDP("udp", udpaddr)
    if err != nil {
        return nil, errors.WithStack(err)
    }

	return conn,err
}
	
