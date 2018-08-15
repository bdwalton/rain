package downloader

import (
	"sync"
	"time"

	"github.com/cenkalti/rain/internal/downloader/piecedownloader"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peermanager"
	"github.com/cenkalti/rain/internal/piece"
	"github.com/cenkalti/rain/internal/torrentdata"
)

const parallelPieceDownloads = 4

type Downloader struct {
	peerManager *peermanager.PeerManager
	data        *torrentdata.Data
	pieces      []Piece
	downloads   map[*piecedownloader.PieceDownloader]struct{}
	log         logger.Logger
	m           sync.Mutex
}

type Piece struct {
	*piece.Piece
	index          int
	havingPeers    map[*peer.Peer]struct{}
	requestedPeers map[*peer.Peer]*piecedownloader.PieceDownloader
}

func New(pm *peermanager.PeerManager, d *torrentdata.Data, l logger.Logger) *Downloader {
	pieces := make([]Piece, len(d.Pieces))
	for i := range d.Pieces {
		pieces[i] = Piece{
			Piece:          &d.Pieces[i],
			index:          i,
			havingPeers:    make(map[*peer.Peer]struct{}),
			requestedPeers: make(map[*peer.Peer]*piecedownloader.PieceDownloader),
		}
	}
	return &Downloader{
		peerManager: pm,
		data:        d,
		pieces:      pieces,
		downloads:   make(map[*piecedownloader.PieceDownloader]struct{}),
		log:         l,
	}
}

func (d *Downloader) Run(stopC chan struct{}) {
	for {
		// TODO extract cases to methods
		select {
		case <-time.After(time.Second):
			for len(d.downloads) < parallelPieceDownloads {
				pi, pe, ok := d.nextDownload()
				if !ok {
					break
				}
				pd := piecedownloader.New(pi, pe)
				d.log.Debugln("downloading piece", pi.Index, "from", pe.String())
				d.m.Lock()
				d.downloads[pd] = struct{}{}
				d.pieces[pi.Index].requestedPeers[pe] = pd
				d.m.Unlock()
				go d.downloadPiece(pd, stopC)
			}
		case pm := <-d.peerManager.PeerMessages():
			switch msg := pm.Message.(type) {
			case peer.Have:
				d.pieces[msg.Index].havingPeers[pm.Peer] = struct{}{}
			case peer.Choke:
				// for _, p := range pieces {
				// 	delete(p.havingPeers, pm.Peer)
				// }
			case peer.Piece:
				// TODO handle piece message
				pd := d.pieces[msg.Piece.Index].requestedPeers[pm.Peer]
				pd.PieceC <- msg

				// 				p.torrent.m.Lock()
				// 				p.torrent.bitfield.Set(piece.Index)
				// 				percentDone := p.torrent.bitfield.Count() * 100 / p.torrent.bitfield.Len()
				// 				p.torrent.m.Unlock()
				// 				p.cond.Broadcast()
				// 				p.torrent.log.Infof("Completed: %d%%", percentDone)
			}
		case <-stopC:
			return
		}
	}
}

func (d *Downloader) nextDownload() (pi *piece.Piece, pe *peer.Peer, ok bool) {
	// TODO selecting pieces in sequential order, change to rarest first
	for _, p := range d.pieces {
		if p.OK {
			continue
		}
		if len(p.havingPeers) == 0 {
			continue
		}
		// TODO selecting first peer having the piece, change to more smart decision
		for pe = range p.havingPeers {
			continue
		}
		if pe == nil {
			continue
		}
		pi = p.Piece
		ok = true
		break
	}
	return
}

func (d *Downloader) downloadPiece(pd *piecedownloader.PieceDownloader, stopC chan struct{}) {
	err := pd.Run(stopC)
	if err != nil {
		d.log.Error(err)
		return
	}
	d.data.Bitfield().Set(pd.Piece.Index)
	d.m.Lock()
	delete(d.downloads, pd)
	delete(d.pieces[pd.Piece.Index].requestedPeers, pd.Peer)
	d.m.Unlock()
}
