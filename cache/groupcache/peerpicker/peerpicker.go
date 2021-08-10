/*
Package peerpicker provides a groupcache.PeerPicker that utilizes a LAN peer discovery
mechanism and sets up the groupcache to use the HTTPPool for communication between
nodes.

Example:
	picker, err := peerpicker.New(7586)
	if err != nil {
		// Do something
	}

	fsys, err := groupcache.New(picker)
	if err != nil {
		// Do something
	}
*/
package peerpicker

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"sort"
	"time"

	"github.com/golang/groupcache"
	jsfs "github.com/johnsiilver/fs"
	"github.com/schollz/peerdiscovery"
)

// IsPeer determines if a discovered peer is a peer for our groupcache.
type IsPeer func(peer peerdiscovery.Discovered) bool

var defaultPayload = []byte(`groupcache`)

func defaultIsPeer(peer peerdiscovery.Discovered) bool {
	return bytes.Equal(peer.Payload, defaultPayload)
}

// LAN provides a groupcache.PeerPicker utilizing schollz peerdiscovery.
type LAN struct {
	*groupcache.HTTPPool

	settings []peerdiscovery.Settings
	iam      string
	isPeer   IsPeer
	peers    []string
	closed   chan struct{}

	serv *http.Server

	logger jsfs.Logger
}

// Option is optional settings for the New() constructor.
type Option func(l *LAN) error

// WithSettings allows passing your own settings for peer discovery. If not specified
// this will go with our own default values for ipv4 and ipv6 (if setup). We default
// to port 9999. iam in the net.IP that you wish to broadcast as. This defaults to
// an IPv6 address on hosts with IPv6.
func WithSettings(iam net.IP, settings []peerdiscovery.Settings, isPeer IsPeer) Option {
	return func(l *LAN) error {
		if len(iam) == 0 {
			return fmt.Errorf("iam must be a valid IP")
		}
		l.settings = settings
		l.isPeer = isPeer
		l.iam = iam.String()

		return nil
	}
}

// WithLogger specifies a logger for us to use.
func WithLogger(logger jsfs.Logger) Option {
	return func(l *LAN) error {
		l.logger = logger
		return nil
	}
}

// New creates a New *LAN instance listening on 'port' for groupcache connections.
func New(port int, options ...Option) (*LAN, error) {
	l := &LAN{
		isPeer: defaultIsPeer,
		logger: jsfs.DefaultLogger{},
	}

	for _, o := range options {
		if err := o(l); err != nil {
			return nil, err
		}
	}

	if len(l.settings) == 0 {
		var err error
		l.iam, l.settings, err = defaultSettings()
		if err != nil {
			return nil, fmt.Errorf("defaultSettings error: %w", err)
		}
	}

	l.HTTPPool = groupcache.NewHTTPPoolOpts(
		"http://"+l.iam,
		&groupcache.HTTPPoolOptions{},
	)

	l.serv = &http.Server{
		Addr:           fmt.Sprintf("%s:%d", l.iam, port),
		Handler:        l.HTTPPool,
		ReadTimeout:    3 * time.Second,
		WriteTimeout:   3 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	go func() {
		l.logger.Println("groupcache peerpicker serving on: ", l.serv.Addr)
		if err := l.serv.ListenAndServe(); err != nil {
			l.logger.Printf("groupcache peerpicker stopped(%s)", l.serv.Addr)
		}
	}()

	go l.discovery()

	return l, nil
}

func defaultSettings() (string, []peerdiscovery.Settings, error) {
	const (
		timeLimit = 3 * time.Second
		delay     = 500 * time.Millisecond
	)

	var ipv4, ipv6 bool
	var settings []peerdiscovery.Settings
	var iam string

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", nil, err
	}
	for _, addr := range addrs {
		ip := addr.(*net.IPNet).IP
		if ip.IsLoopback() || ip.IsMulticast() || ip.IsUnspecified() {
			continue
		}

		if ip.To4() == nil {
			ipv6 = true
			iam = ip.String()
			continue
		}
		ipv4 = true
		if !ipv6 {
			iam = ip.String()
		}
	}
	if ipv4 {
		settings = append(
			settings,
			peerdiscovery.Settings{
				TimeLimit: timeLimit,
				IPVersion: peerdiscovery.IPv4,
				Delay:     delay,
				Payload:   defaultPayload,
			},
		)
	}
	if ipv6 {
		settings = append(
			settings,
			peerdiscovery.Settings{
				TimeLimit: timeLimit,
				IPVersion: peerdiscovery.IPv4,
				Delay:     delay,
				Payload:   defaultPayload,
			},
		)
	}
	if len(settings) == 0 {
		return iam, nil, fmt.Errorf("neither IPv4 or IPv6 exists on the machine")
	}
	return iam, settings, nil
}

func (l *LAN) Close() {
	close(l.closed)
	l.serv.Shutdown(context.Background())
}

func (l *LAN) discovery() {
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-l.closed:
			return
		case <-tick.C:
		}

		peers, err := peerdiscovery.Discover(l.settings...)
		if err != nil {
			l.logger.Printf("groupcache peerdiscovery: %s", err)
			continue
		}

		l.setPeers(peers)
	}
}

func (l *LAN) setPeers(peers []peerdiscovery.Discovered) {
	peerList := []string{}

	for _, peer := range peers {
		if l.isPeer(peer) {
			peerList = append(peerList, "http://"+peer.Address)
		}
	}

	peerList = sort.StringSlice(peerList)

	// If we don't have the same length of peers, we know the peer list is different.
	if len(peerList) != len(l.peers) {
		l.peers = peerList
		l.HTTPPool.Set(peerList...)
		return
	}

	// If any peer at an index is different, update our set of peers.
	for i, addr := range peerList {
		if l.peers[i] != addr {
			l.peers = peerList
			l.HTTPPool.Set(peerList...)
		}
	}
}
