package peer

import (
	"github.com/cenkalti/rain/torrent/internal/peerconn"
	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
)

type Peer struct {
	*peerconn.Conn
	messages   chan Message
	disconnect chan *Peer

	AmChoking                    bool
	AmInterested                 bool
	PeerChoking                  bool
	PeerInterested               bool
	BytesDownlaodedInChokePeriod int64
	OptimisticUnchoked           bool

	// Snubbed means peer is sending pieces too slow.
	Snubbed bool

	// Messages received while we don't have info yet are saved here.
	Messages []interface{}

	ExtensionHandshake *peerprotocol.ExtensionHandshakeMessage

	AllowedFastPieces map[uint32]struct{}

	closeC chan struct{}
	doneC  chan struct{}
}

type Message struct {
	*Peer
	Message interface{}
}

func New(p *peerconn.Conn, messages chan Message, disconnect chan *Peer) *Peer {
	return &Peer{
		Conn:              p,
		messages:          messages,
		disconnect:        disconnect,
		AmChoking:         true,
		PeerChoking:       true,
		AllowedFastPieces: make(map[uint32]struct{}),
		closeC:            make(chan struct{}),
		doneC:             make(chan struct{}),
	}
}

func (p *Peer) Close() {
	close(p.closeC)
	p.Conn.Close()
	<-p.doneC
}

func (p *Peer) Run() {
	defer close(p.doneC)
	go p.Conn.Run()
	for msg := range p.Conn.Messages() {
		select {
		case p.messages <- Message{Peer: p, Message: msg}:
		case <-p.closeC:
			return
		}
	}
	select {
	case p.disconnect <- p:
	case <-p.closeC:
		return
	}
}

type ByDownloadRate []*Peer

func (a ByDownloadRate) Len() int      { return len(a) }
func (a ByDownloadRate) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByDownloadRate) Less(i, j int) bool {
	return a[i].BytesDownlaodedInChokePeriod > a[j].BytesDownlaodedInChokePeriod
}
