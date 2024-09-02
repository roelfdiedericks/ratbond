package main
import (
	"github.com/roelfdiedericks/kcp-go"
	"net"
	"time"
	"errors"
	"golang.org/x/crypto/pbkdf2"
	"crypto/sha1"
)
type RatSession struct {
	convid uint32

	udp_conn       *net.UDPConn // the underlying packet connection
	remote     net.Addr  // remote peer address

	kcp *kcp.UDPSession

	// settings
	
}

// NewSession establishes a session and talks KCP protocol over a packet connection.
func NewSession(convid uint32, l_udpaddr *net.UDPAddr,l_conn *net.UDPConn) (*RatSession, error) {
	if (convid==0) {
		l.Panicf("convid = 0 !")
	}
	session:=new(RatSession)
	session.convid=convid
	

	key := pbkdf2.Key([]byte(g_secret), []byte("ratbond salt"), 1024, 32, sha1.New)
	var block kcp.BlockCrypt
	if (g_use_aes) {
		block, _ = kcp.NewAESBlockCrypt(key)
	} else {
		block, _ = kcp.NewNoneBlockCrypt(key)
	}

	kcp,err:=kcp.NewConn3(convid,l_udpaddr,block,0,0,l_conn)

	if err!=nil {
		return nil,err
	}
	
	session.kcp=kcp
	session.udp_conn=l_conn
	l.Tracef("NewSession: %+v",session)
	l.Warnf("kcp segment size:%d",kcp.GetSegmentSize())

	session.setKCPOptions(session.kcp) //TODO, fixme

	return session,nil
}

// Read implements net.Conn
func (s *RatSession) Read(b []byte) (n int, err error) {
	//s.kcp.SetReadDeadline(time.Now().Add(time.Millisecond*2000))
	if s==nil {
		return 0,errors.New("nil session")
	}
	return s.kcp.Read(b)
}

// Write implements net.Conn
func (s *RatSession) Write(b []byte) (n int, err error) { 
	//s.kcp.SetWriteDeadline(time.Now().Add(time.Millisecond*g_write_deadline))
	if s==nil {
		return 0,errors.New("nil session")
	}
	return s.kcp.Write(b)
}

func (s *RatSession) Close() error {
	if s==nil {
		return errors.New("nil session")
	}
	if (s.udp_conn!=nil) {
		s.udp_conn.Close()
	}
	return s.kcp.Close()
}
	
func (s *RatSession) GetState() uint32 { 
	if s==nil {
		return 0
	}
	return s.kcp.GetState() 
}


func (s *RatSession) SetReadBuffer(bytes int) error {
	if s==nil {
		return errors.New("nil session")
	}
	return s.kcp.SetReadBuffer(bytes)
}

func (s *RatSession) SetWriteBuffer(bytes int) error {
	if s==nil {
		return errors.New("nil session")
	}
	return s.kcp.SetWriteBuffer(bytes)
}


// SetReadDeadline implements the Conn SetReadDeadline method.
func (s *RatSession) SetReadDeadline(t time.Time) error {
	if s==nil {
		return errors.New("nil session")
	}
	return s.kcp.SetReadDeadline(t)
}
// SetWriteDeadline implements the Conn SetWriteDeadline method.
func (s *RatSession) SetWriteDeadline(t time.Time) error {
	if s==nil {
		return errors.New("nil session")
	}
	return s.kcp.SetWriteDeadline(t)
}

// RemoteAddr returns the remote network address. The Addr returned is shared by all invocations of RemoteAddr, so do not modify it.
func (s *RatSession) RemoteAddr() net.Addr { 
	if s!=nil {
		return s.remote 
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

func (s *RatSession) setKCPOptions(conn *kcp.UDPSession) {
	if (s.kcp!=nil) {
		NoDelay, Interval, Resend, NoCongestion := 1, 10, 2, 1 //useful mode
		//NoDelay, Interval, Resend, NoCongestion := 1, 10, 2, 1 //turbo mode
		//NoDelay, Interval, Resend, NoCongestion := 0, 40, 0, 0 //normal mode
		MTU:=g_kcp_mtu
		SndWnd:=2048 //2048 seems good for thruput
		RcvWnd:=2048
		AckNodelay:=false //this is more speedy
		s.kcp.SetStreamMode(false) //message mode or stream mode, warrants more checking, stream mode seems faster which is interesting
		s.kcp.SetWriteDelay(false)
		s.kcp.SetNoDelay(NoDelay, Interval, Resend, NoCongestion)
		s.kcp.SetMtu(MTU)
		s.kcp.SetWindowSize(SndWnd, RcvWnd)
		s.kcp.SetACKNoDelay(AckNodelay)
	}
}

// ========================== LISTENER ===========================
// ================================================================
func createListener() (*kcp.Listener, error) {
	l.Debugf("createListener")
		key := pbkdf2.Key([]byte(g_secret), []byte("ratbond salt"), 1024, 32, sha1.New)
		var block kcp.BlockCrypt
		if (g_use_aes) {
			block, _ = kcp.NewAESBlockCrypt(key)
		} else {
			block, _ = kcp.NewNoneBlockCrypt(key)
		}

		l.Infof("listening on %s",g_listen_addr)
		//return net.Listen("tcp", g_listen_addr)
		listener,err:=kcp.ListenWithOptions(g_listen_addr, block, 0, 0);
		return listener,err
}
	