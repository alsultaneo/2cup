package main

import (
	"bytes"
	"compress/gzip"
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png" // enable PNG decoding in image.Decode
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"golang.org/x/crypto/nacl/box"
	"golang.org/x/crypto/nacl/secretbox"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	ma "github.com/multiformats/go-multiaddr"
	mh "github.com/multiformats/go-multihash"

	"golang.org/x/sys/windows/registry"
)

// ─────────────────────────────────────────────────────────────────────────────
// Win95 Palette
// ─────────────────────────────────────────────────────────────────────────────
var (
	w95Silver    = color.NRGBA{R: 192, G: 192, B: 192, A: 255}
	w95DkGray    = color.NRGBA{R: 128, G: 128, B: 128, A: 255}
	w95Darker    = color.NRGBA{R: 64, G: 64, B: 64, A: 255}
	w95White     = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	w95Desktop   = color.NRGBA{R: 0, G: 128, B: 128, A: 255}
	w95TitleBar  = color.NRGBA{R: 0, G: 0, B: 128, A: 255}
	w95TitleText = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	w95Black     = color.NRGBA{R: 0, G: 0, B: 0, A: 255}
	w95InputBg   = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	w95Yellow    = color.NRGBA{R: 255, G: 213, B: 0, A: 255}
	w95YellowDk  = color.NRGBA{R: 180, G: 148, B: 0, A: 255}
	w95YellowLt  = color.NRGBA{R: 255, G: 241, B: 140, A: 255}
	w95Green     = color.NRGBA{R: 0, G: 170, B: 0, A: 255}
	w95Red       = color.NRGBA{R: 200, G: 0, B: 0, A: 255}
	clrTransp    = color.Transparent
)

// ─────────────────────────────────────────────────────────────────────────────
// Win95 Theme
// ─────────────────────────────────────────────────────────────────────────────
type win95Theme struct{}

func (win95Theme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameBackground:
		return w95Silver
	case theme.ColorNameButton:
		return w95Silver
	case theme.ColorNameForeground:
		return w95Black
	case theme.ColorNameInputBackground:
		return w95InputBg
	case theme.ColorNamePrimary:
		return w95Yellow
	case theme.ColorNamePlaceHolder:
		return w95DkGray
	case theme.ColorNameSeparator:
		return w95DkGray
	case theme.ColorNameFocus:
		return w95Yellow
	}
	return theme.DefaultTheme().Color(n, v)
}
func (win95Theme) Font(s fyne.TextStyle) fyne.Resource     { return theme.DefaultTheme().Font(s) }
func (win95Theme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }
func (win95Theme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameText:
		return 13
	case theme.SizeNamePadding:
		return 4
	case theme.SizeNameInnerPadding:
		return 3
	}
	return theme.DefaultTheme().Size(n)
}

// ─────────────────────────────────────────────────────────────────────────────
// UI primitives
// ─────────────────────────────────────────────────────────────────────────────
func raised3D(content fyne.CanvasObject) fyne.CanvasObject {
	face := canvas.NewRectangle(w95Silver)
	face.StrokeColor = w95DkGray
	face.StrokeWidth = 2
	return container.NewStack(face, container.NewPadded(content))
}
func sunken3D(content fyne.CanvasObject) fyne.CanvasObject {
	face := canvas.NewRectangle(w95InputBg)
	face.StrokeColor = w95DkGray
	face.StrokeWidth = 2
	return container.NewStack(face, container.NewPadded(content))
}
func titleBar(title string) fyne.CanvasObject {
	bg := canvas.NewRectangle(w95TitleBar)
	bg.SetMinSize(fyne.NewSize(0, 26))
	icon := canvas.NewRectangle(w95Yellow)
	icon.SetMinSize(fyne.NewSize(16, 16))
	iconBdr := canvas.NewRectangle(w95YellowDk)
	iconBdr.StrokeColor = w95YellowDk
	iconBdr.StrokeWidth = 1
	iconWidget := container.NewStack(iconBdr, icon)
	titleTxt := canvas.NewText(" "+title, w95TitleText)
	titleTxt.TextStyle = fyne.TextStyle{Bold: true}
	titleTxt.TextSize = 13
	makeCtrlBtn := func(label string) fyne.CanvasObject {
		face := canvas.NewRectangle(w95Silver)
		face.StrokeColor = w95DkGray
		face.StrokeWidth = 1
		face.SetMinSize(fyne.NewSize(18, 18))
		lbl := canvas.NewText(label, w95Black)
		lbl.TextSize = 10
		lbl.TextStyle = fyne.TextStyle{Bold: true}
		return container.NewStack(face, container.NewCenter(lbl))
	}
	ctrlRow := container.NewHBox(makeCtrlBtn("_"), makeCtrlBtn("□"), makeCtrlBtn("X"))
	titleRow := container.NewBorder(nil, nil,
		container.NewHBox(spacer95(4, 0), iconWidget, spacer95(4, 0), titleTxt),
		container.NewHBox(ctrlRow, spacer95(4, 0)),
	)
	return container.NewStack(bg, container.NewPadded(titleRow))
}
func ctext95(s string, c color.Color, sz float32, bold bool) *canvas.Text {
	t := canvas.NewText(s, c)
	t.TextSize = sz
	t.TextStyle = fyne.TextStyle{Bold: bold, Monospace: true}
	return t
}
func spacer95(w, h float32) fyne.CanvasObject {
	r := canvas.NewRectangle(clrTransp)
	r.SetMinSize(fyne.NewSize(w, h))
	return r
}
func hline95() fyne.CanvasObject {
	top := canvas.NewRectangle(w95DkGray)
	top.SetMinSize(fyne.NewSize(0, 1))
	bot := canvas.NewRectangle(w95White)
	bot.SetMinSize(fyne.NewSize(0, 1))
	return container.NewVBox(top, bot)
}

// ─────────────────────────────────────────────────────────────────────────────
// Encryption helpers
// ─────────────────────────────────────────────────────────────────────────────
func roomKey(code, password string) *[32]byte {
	h := sha256.Sum256([]byte("p2p-e2e-v1-" + code + "-" + password))
	return &h
}
func encrypt(key *[32]byte, plain []byte) []byte {
	var nonce [24]byte
	if _, err := crand.Read(nonce[:]); err != nil {
		return nil
	}
	return secretbox.Seal(nonce[:], plain, &nonce, key)
}
func decrypt(key *[32]byte, enc []byte) ([]byte, bool) {
	if len(enc) < 25 {
		return nil, false
	}
	var nonce [24]byte
	copy(nonce[:], enc[:24])
	return secretbox.Open(nil, enc[24:], &nonce, key)
}

// ─────────────────────────────────────────────────────────────────────────────
// Compression
// ─────────────────────────────────────────────────────────────────────────────
func compressData(data []byte) []byte {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	if _, err := gz.Write(data); err != nil {
		return data
	}
	gz.Close()
	return b.Bytes()
}
func decompressData(data []byte) []byte {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return data
	}
	defer r.Close()
	res, _ := io.ReadAll(r)
	return res
}
func compressImage(imgBytes []byte) []byte {
	img, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return imgBytes
	}
	var buf bytes.Buffer
	opt := jpeg.Options{Quality: 60}
	err = jpeg.Encode(&buf, img, &opt)
	if err != nil {
		return imgBytes
	}
	return buf.Bytes()
}

// ─────────────────────────────────────────────────────────────────────────────
// Windows MCI voice recording/playback
// ─────────────────────────────────────────────────────────────────────────────
var (
	winmm          = syscall.NewLazyDLL("winmm.dll")
	mciSendStringW = winmm.NewProc("mciSendStringW")
)

// ─────────────────────────────────────────────────────────────────────────────
// ADDED: hide the console/cmd window that Windows pops up when the binary is
// built with the default (console) subsystem. The real fix is to build with
//
//	go build -ldflags "-H=windowsgui" -o 2cup.exe .
//
// which stops Windows from ever allocating a console for this process. This
// runtime hide is a safety net for people who build with a plain `go build`
// (or via `go run`, or a script that forgot the ldflags) — it hides the
// console window a moment after the process starts, so it doesn't hang
// around as an empty black box for the rest of the session.
// ─────────────────────────────────────────────────────────────────────────────
var (
	kernel32dll           = syscall.NewLazyDLL("kernel32.dll")
	user32dll             = syscall.NewLazyDLL("user32.dll")
	procGetConsoleWindow  = kernel32dll.NewProc("GetConsoleWindow")
	procGetWindowThreadPI = user32dll.NewProc("GetWindowThreadProcessId")
	procShowWindowConsole = user32dll.NewProc("ShowWindow")
)

const swHideConsole = 0

func hideOwnConsoleWindow() {
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd == 0 {
		return // no console attached at all (e.g. already built with -H=windowsgui)
	}
	var consolePID uint32
	procGetWindowThreadPI.Call(hwnd, uintptr(unsafe.Pointer(&consolePID)))
	if consolePID != uint32(syscall.Getpid()) {
		// The console belongs to a different process (e.g. we were launched
		// from an existing terminal the user is still using) - don't touch it.
		return
	}
	procShowWindowConsole.Call(hwnd, uintptr(swHideConsole))
}

func mci(cmd string) {
	p, _ := syscall.UTF16PtrFromString(cmd)
	mciSendStringW.Call(uintptr(unsafe.Pointer(p)), 0, 0, 0)
}

var voiceFile = filepath.Join(os.TempDir(), "p2pmsg_voice.wav")

func startRecord() {
	mci("close p2pcap")
	mci("open new Type waveaudio Alias p2pcap")
	mci("set p2pcap time format milliseconds")
	mci("record p2pcap")
}
func stopRecord() {
	mci(fmt.Sprintf(`save p2pcap "%s"`, voiceFile))
	mci("close p2pcap")
}
func playWAV(path string) {
	go func() {
		alias := fmt.Sprintf("p2pplay%d", time.Now().UnixNano())
		mci(fmt.Sprintf(`open "%s" type waveaudio alias %s`, path, alias))
		mci(fmt.Sprintf("play %s wait", alias))
		mci(fmt.Sprintf("close %s", alias))
	}()
}

// ─────────────────────────────────────────────────────────────────────────────
// P2P types
// ─────────────────────────────────────────────────────────────────────────────
type NetBlob struct {
	E string `json:"e"`
}
type InnerPayload struct {
	Hash string `json:"h"`
	Name string `json:"n"`
	Type string `json:"t"`
	Data string `json:"d"`
}

type ChatMsg struct {
	SenderID   string
	SenderName string
	Text       string
	ImgBytes   []byte
	VoicePath  string
	IsMine     bool
	IsSys      bool
	At         time.Time
}

type PeerInfo struct {
	ID, Name string
	Seen     time.Time
	Online   bool
}

type ChatApp struct {
	w         fyne.Window
	msgBox    *fyne.Container
	msgScroll *container.Scroll
	peerBox   *fyne.Container
	peers     map[string]*PeerInfo
	mu        sync.RWMutex
	input     *widget.Entry
	charLbl   *canvas.Text
	statusLbl *widget.Label
	recBtn    *widget.Button
	recording bool
	recStart  time.Time
	recCtx    context.Context
	recCancel context.CancelFunc
	myID      string
	myName    string
	roomCode  string
	eKey      *[32]byte
	roomTopic *pubsub.Topic
	roomSub   *pubsub.Subscription
}

// ─────────────────────────────────────────────────────────────────────────────
// Global singleton for the single libp2p host
// ─────────────────────────────────────────────────────────────────────────────
var (
	globalHost    host.Host
	globalDHT     *dht.IpfsDHT
	globalPS      *pubsub.PubSub
	globalCtx     context.Context
	globalCancel  context.CancelFunc
	globalPing    *ping.PingService
	globalConnMgr *connmgr.BasicConnMgr

	dmMu             sync.Mutex
	dmListenerCtx    context.Context
	dmListenerCancel context.CancelFunc
	dmListenerSub    *pubsub.Subscription // ADDED: To explicitly cancel subscription on logout

	dmTopicMu    sync.Mutex
	dmTopicCache = map[string]*pubsub.Topic{}
)

var (
	cleanupMu    sync.Mutex
	cleanupFns   []func()
	shutdownOnce sync.Once
)

func registerCleanup(fn func()) {
	cleanupMu.Lock()
	defer cleanupMu.Unlock()
	cleanupFns = append(cleanupFns, fn)
}

func runShutdown() {
	shutdownOnce.Do(func() {
		cleanupMu.Lock()
		fns := append([]func(){}, cleanupFns...)
		cleanupMu.Unlock()
		for _, fn := range fns {
			fn()
		}
		if globalCancel != nil {
			globalCancel()
		}
		if globalHost != nil {
			globalHost.Close()
		}
	})
}

func getOrJoinTopic(name string) (*pubsub.Topic, error) {
	dmTopicMu.Lock()
	defer dmTopicMu.Unlock()
	if t, ok := dmTopicCache[name]; ok {
		return t, nil
	}
	t, err := globalPS.Join(name)
	if err != nil {
		return nil, err
	}
	dmTopicCache[name] = t
	return t, nil
}

func mustAddrInfos(maddrs []ma.Multiaddr) []peer.AddrInfo {
	var out []peer.AddrInfo
	for _, m := range maddrs {
		if ai, err := peer.AddrInfoFromP2pAddr(m); err == nil {
			out = append(out, *ai)
		}
	}
	return out
}
func shortID(id peer.ID) string {
	s := id.String()
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
func bootstrap(ctx context.Context) {
	var wg sync.WaitGroup
	for _, pi := range mustAddrInfos(dht.DefaultBootstrapPeers) {
		wg.Add(1)
		go func(ai peer.AddrInfo) {
			defer wg.Done()
			c, cancel := context.WithTimeout(ctx, 12*time.Second)
			defer cancel()
			_ = globalHost.Connect(c, ai)
		}(pi)
	}
	wg.Wait()
}

// ─────────────────────────────────────────────────────────────────────────────
// ADDED: dynamic relay mesh.
//
// Why this is needed: two peers on the same Wi-Fi can dial each other
// directly because they're on the same LAN / behind the same NAT device.
// The moment one of them is on mobile data, it's very likely sitting behind
// carrier-grade NAT (CGNAT) that simply does not allow *any* unsolicited
// inbound connection - no amount of UPnP/port-forwarding on the other side
// fixes that, because the mobile peer has no public address to be reached
// on at all. libp2p's answer to this is a circuit relay: both peers open an
// outbound connection to a third, publicly reachable peer, which then
// relays traffic between them (and, ideally, helps them hole-punch a
// direct connection afterwards).
//
// The previous code pointed AutoRelay at the IPFS DHT bootstrap peers, but
// those nodes are DHT bootstrappers, not circuit-relay-v2 relays, so that
// call was silently a no-op - there was never an actual working relay.
//
// The fix below has two halves:
//  1. relayPeerSource: feeds AutoRelay a list of *real* relay candidates,
//     discovered dynamically via the DHT under a rendezvous string,
//     instead of the bootstrap peers.
//  2. runSelfRelayAdvertiser: any 2cup instance that libp2p's own NAT
//     detection (AutoNAT) determines is publicly reachable (e.g. it's
//     running on a VPS, or its router has a forwarded port) starts
//     advertising itself under that same rendezvous string, so it becomes
//     available as a relay for everyone else - including peers on mobile
//     data / behind CGNAT.
//
// Important caveat: this only helps once at least one 2cup peer somewhere
// is actually publicly reachable. If nobody is, there is nothing to relay
// through and cross-network connections will still fail - that's a hard
// limitation of pure P2P, not something any code change can paper over.
// For guaranteed reliability, run a dedicated always-on relay (any small
// VPS with a public IP works) and point AutoRelay at it explicitly.
// ─────────────────────────────────────────────────────────────────────────────
const relayRendezvous = "2cup-relay-v1"

// relayPeerSource is passed to libp2p.EnableAutoRelayWithPeerSource. It is
// called by AutoRelay whenever it needs candidate relays, and returns peers
// that have advertised themselves as relay-capable via the DHT.
func relayPeerSource(ctx context.Context, num int) <-chan peer.AddrInfo {
	out := make(chan peer.AddrInfo)
	go func() {
		defer close(out)
		if globalDHT == nil {
			return
		}
		disc := routing.NewRoutingDiscovery(globalDHT)
		fctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		peers, err := disc.FindPeers(fctx, relayRendezvous)
		if err != nil {
			return
		}
		sent := 0
		for p := range peers {
			if p.ID == globalHost.ID() || len(p.Addrs) == 0 {
				continue
			}
			select {
			case out <- p:
				sent++
				if sent >= num {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// runSelfRelayAdvertiser watches this host's own NAT reachability (as
// determined by libp2p's built-in AutoNAT) and, whenever this peer is
// publicly reachable, advertises it on the DHT as a relay so other 2cup
// peers stuck behind NAT/CGNAT can use it.
func runSelfRelayAdvertiser(ctx context.Context) {
	sub, err := globalHost.EventBus().Subscribe(new(event.EvtLocalReachabilityChanged))
	if err != nil {
		log.Println("could not subscribe to reachability events:", err)
		return
	}
	defer sub.Close()

	disc := routing.NewRoutingDiscovery(globalDHT)
	var advCancel context.CancelFunc

	stopAdvertising := func() {
		if advCancel != nil {
			advCancel()
			advCancel = nil
		}
	}
	startAdvertising := func() {
		if advCancel != nil {
			return // already advertising
		}
		var actx context.Context
		actx, advCancel = context.WithCancel(ctx)
		go func() {
			for {
				select {
				case <-actx.Done():
					return
				default:
				}
				ttl, err := disc.Advertise(actx, relayRendezvous)
				if err != nil {
					select {
					case <-actx.Done():
						return
					case <-time.After(15 * time.Second):
						continue
					}
				}
				w := ttl - 5*time.Second
				if w < 5*time.Second {
					w = 5 * time.Second
				}
				select {
				case <-actx.Done():
					return
				case <-time.After(w):
				}
			}
		}()
		log.Println("this peer looks publicly reachable — now advertising itself as a relay for other 2cup users")
	}

	for {
		select {
		case <-ctx.Done():
			stopAdvertising()
			return
		case e, ok := <-sub.Out():
			if !ok {
				return
			}
			ev, ok := e.(event.EvtLocalReachabilityChanged)
			if !ok {
				continue
			}
			if ev.Reachability == network.ReachabilityPublic {
				startAdvertising()
			} else {
				stopAdvertising()
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Chat bubble / peer row
// ─────────────────────────────────────────────────────────────────────────────
func makeBubble(m ChatMsg) fyne.CanvasObject {
	if m.IsSys {
		bg := canvas.NewRectangle(w95Yellow)
		bg.StrokeColor = w95YellowDk
		bg.StrokeWidth = 1
		icon := ctext95("[i]", w95Black, 11, true)
		lbl := widget.NewLabelWithStyle("  "+m.Text, fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
		row := container.NewHBox(icon, lbl)
		return container.NewStack(bg, container.NewPadded(row))
	}
	nameClr := w95TitleBar
	if m.IsMine {
		nameClr = w95YellowDk
	}
	nameT := ctext95(m.SenderName, nameClr, 11, true)
	tsT := ctext95(m.At.Format("15:04:05"), w95DkGray, 10, false)

	var bgClr, strokeClr color.Color
	if m.IsMine {
		bgClr = w95YellowLt
		strokeClr = w95YellowDk
	} else {
		bgClr = w95White
		strokeClr = w95DkGray
	}
	bg := canvas.NewRectangle(bgClr)
	bg.StrokeColor = strokeClr
	bg.StrokeWidth = 1

	var innerContent fyne.CanvasObject
	switch {
	case len(m.ImgBytes) > 0:
		img := canvas.NewImageFromReader(bytes.NewReader(m.ImgBytes), "img")
		img.FillMode = canvas.ImageFillContain
		img.SetMinSize(fyne.NewSize(220, 150))
		innerContent = container.NewVBox(
			container.NewHBox(nameT, spacer95(8, 0), tsT),
			img,
		)
	case m.VoicePath != "":
		dur := ""
		if fi, err := os.Stat(m.VoicePath); err == nil {
			secs := fi.Size() / 32000
			dur = fmt.Sprintf(" [%ds]", secs)
		}
		playBtn := widget.NewButton("► Play WAV"+dur, func() { playWAV(m.VoicePath) })
		waveIcon := ctext95("~(((-<", w95YellowDk, 13, true)
		innerContent = container.NewVBox(
			container.NewHBox(nameT, spacer95(8, 0), tsT),
			container.NewHBox(waveIcon, playBtn),
		)
	default:
		body := widget.NewLabel(m.Text)
		body.Wrapping = fyne.TextWrapWord
		innerContent = container.NewVBox(
			container.NewHBox(nameT, spacer95(8, 0), tsT),
			body,
		)
	}
	bubble := container.NewStack(bg, container.NewPadded(innerContent))
	sp := spacer95(100, 1)
	if m.IsMine {
		return container.NewBorder(nil, nil, sp, nil, bubble)
	}
	return container.NewBorder(nil, nil, nil, sp, bubble)
}

func makePeerRow(p *PeerInfo) fyne.CanvasObject {
	dotClr := w95Red
	dotLabel := "[X]"
	if p.Online {
		dotClr = w95Green
		dotLabel = "[+]"
	}
	dot := ctext95(dotLabel, dotClr, 11, true)
	name := widget.NewLabelWithStyle(p.Name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true, Monospace: true})
	idlbl := widget.NewLabelWithStyle(p.ID, fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	bg := canvas.NewRectangle(w95Silver)
	bg.StrokeColor = w95DkGray
	bg.StrokeWidth = 1
	row := container.NewBorder(nil, nil, container.NewHBox(spacer95(2, 0), dot, spacer95(4, 0)), nil,
		container.NewVBox(name, idlbl))
	return container.NewStack(bg, container.NewPadded(row))
}

// ─────────────────────────────────────────────────────────────────────────────
// ChatApp methods
// ─────────────────────────────────────────────────────────────────────────────

func (ca *ChatApp) add(senderID, senderName, text string, imgB []byte, voicePath string, mine, sys bool) {
	m := ChatMsg{senderID, senderName, text, imgB, voicePath, mine, sys, time.Now()}
	fyne.Do(func() {
		ca.msgBox.Add(makeBubble(m))
		ca.msgBox.Add(spacer95(0, 3))
		ca.msgBox.Refresh()
		ca.msgScroll.ScrollToBottom()
	})
}
func (ca *ChatApp) sysMsg(text string) { ca.add("", "System", text, nil, "", false, true) }

func (ca *ChatApp) updatePeer(id, name string) {
	ca.mu.Lock()
	_, existed := ca.peers[id]
	if existed {
		p := ca.peers[id]
		p.Seen = time.Now()
		p.Online = true
		if name != "" {
			p.Name = name
		}
	} else {
		dn := name
		if dn == "" {
			dn = id
		}
		ca.peers[id] = &PeerInfo{id, dn, time.Now(), true}
	}
	ca.mu.Unlock()

	if !existed {
		dn := name
		if dn == "" {
			dn = id
		}
		ca.sysMsg(dn + " has entered the chat")
	}
	ca.rebuildPeers()
}

func (ca *ChatApp) rebuildPeers() {
	ca.mu.RLock()
	peersCopy := make([]*PeerInfo, 0, len(ca.peers))
	for _, p := range ca.peers {
		peersCopy = append(peersCopy, p)
	}
	ca.mu.RUnlock()

	fyne.Do(func() {
		ca.peerBox.RemoveAll()
		for _, p := range peersCopy {
			ca.peerBox.Add(makePeerRow(p))
			ca.peerBox.Add(spacer95(0, 2))
		}
		ca.peerBox.Refresh()
	})
}

func (ca *ChatApp) checkOffline() {
	ca.mu.Lock()
	type g struct{ id, name string }
	var gone []g
	for id, p := range ca.peers {
		if p.Online && time.Since(p.Seen) > 180*time.Second {
			p.Online = false
			gone = append(gone, g{id, p.Name})
		}
	}
	ca.mu.Unlock()
	for _, x := range gone {
		ca.sysMsg(x.name + " has left the chat")
	}
	if len(gone) > 0 {
		ca.rebuildPeers()
	}
}

func (ca *ChatApp) publish(msgType, payload string) {
	inner := InnerPayload{Hash: ca.myID, Name: ca.myName, Type: msgType, Data: payload}
	plain, _ := json.Marshal(inner)
	enc := encrypt(ca.eKey, plain)
	if enc == nil {
		return
	}
	blob := NetBlob{E: base64.StdEncoding.EncodeToString(enc)}
	b, _ := json.Marshal(blob)
	ctx, cancel := context.WithTimeout(globalCtx, 10*time.Second)
	_ = ca.roomTopic.Publish(ctx, b)
	cancel()
}

func (ca *ChatApp) sendText() {
	text := ca.input.Text
	if len([]rune(text)) == 0 {
		return
	}
	if len([]rune(text)) > 500 {
		ca.sysMsg("ERROR: Message too long (max 500 chars)")
		return
	}
	ca.input.SetText("")
	if ca.charLbl != nil {
		ca.charLbl.Text = "0/500"
		ca.charLbl.Refresh()
	}
	go func() {
		ca.publish("msg", text)
		ca.add(ca.myID, ca.myName, text, nil, "", true, false)
	}()
}

func (ca *ChatApp) sendImage() {
	fd := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
		if err != nil || r == nil {
			return
		}
		defer r.Close()
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, r); err != nil {
			ca.sysMsg("ERROR: Failed to read image")
			return
		}
		imgBytes := compressImage(buf.Bytes())
		if len(imgBytes) > 3*1024*1024 {
			ca.sysMsg("ERROR: Image too large (max 3 MB)")
			return
		}
		b64 := base64.StdEncoding.EncodeToString(imgBytes)
		go func() {
			ca.publish("img", b64)
			ca.add(ca.myID, ca.myName, "", imgBytes, "", true, false)
		}()
	}, ca.w)
	fd.SetFilter(storage.NewExtensionFileFilter([]string{".png", ".jpg", ".jpeg", ".gif", ".webp"}))
	fd.Show()
}

func (ca *ChatApp) finishAndSendRecord() {
	stopRecord()
	b, err := os.ReadFile(voiceFile)
	if err != nil || len(b) == 0 {
		ca.sysMsg("ERROR: No audio recorded")
		fyne.Do(func() {
			ca.recording = false
			ca.recBtn.SetText("[MIC]")
		})
		return
	}
	b = compressData(b)
	if len(b) > 4*1024*1024 {
		ca.sysMsg("ERROR: Recording too long (max ~2 min)")
		fyne.Do(func() {
			ca.recording = false
			ca.recBtn.SetText("[MIC]")
		})
		return
	}
	dur := int(time.Since(ca.recStart).Seconds())
	b64 := base64.StdEncoding.EncodeToString(b)
	ca.publish("voice", fmt.Sprintf("%d|%s", dur, b64))

	fyne.Do(func() {
		ca.recording = false
		ca.recBtn.SetText("[MIC]")
		ca.add(ca.myID, ca.myName, "", nil, voiceFile, true, false)
	})
}

func (ca *ChatApp) toggleRecord() {
	if !ca.recording {
		ca.recording = true
		ca.recStart = time.Now()
		ca.recCtx, ca.recCancel = context.WithCancel(context.Background())
		startRecord()
		ca.recBtn.SetText("[ STOP ]")

		go func() {
			select {
			case <-ca.recCtx.Done():
				return
			case <-time.After(30 * time.Second):
				ca.finishAndSendRecord()
			}
		}()
	} else {
		if ca.recCancel != nil {
			ca.recCancel()
		}
		go ca.finishAndSendRecord()
	}
}

func (ca *ChatApp) ping() { ca.publish("ping", "") }

// ─────────────────────────────────────────────────────────────────────────────
// P2P reader (room subscription)
// ─────────────────────────────────────────────────────────────────────────────
func (ca *ChatApp) reader(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		raw, err := ca.roomSub.Next(ctx)
		if err != nil {
			return
		}
		if raw.ReceivedFrom == globalHost.ID() {
			continue
		}
		var blob NetBlob
		if err := json.Unmarshal(raw.Data, &blob); err != nil {
			continue
		}
		enc, err := base64.StdEncoding.DecodeString(blob.E)
		if err != nil {
			continue
		}
		dec, ok := decrypt(ca.eKey, enc)
		if !ok {
			continue
		}
		var inner InnerPayload
		if err := json.Unmarshal(dec, &inner); err != nil {
			continue
		}
		ca.updatePeer(inner.Hash, inner.Name)
		switch inner.Type {
		case "ping", "join":
		case "msg":
			ca.add(inner.Hash, inner.Name, inner.Data, nil, "", false, false)
		case "img":
			imgB, err := base64.StdEncoding.DecodeString(inner.Data)
			if err == nil {
				ca.add(inner.Hash, inner.Name, "", imgB, "", false, false)
			}
		case "voice":
			s := inner.Data
			sep := strings.IndexByte(s, '|')
			if sep < 0 {
				continue
			}
			wavB, _ := base64.StdEncoding.DecodeString(s[sep+1:])
			wavB = decompressData(wavB)
			vpath := filepath.Join(os.TempDir(), fmt.Sprintf("p2pvoice_%d.wav", time.Now().UnixNano()))
			if err := os.WriteFile(vpath, wavB, 0644); err == nil {
				ca.add(inner.Hash, inner.Name, "", nil, vpath, false, false)
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Room discovery
// ─────────────────────────────────────────────────────────────────────────────
func (ca *ChatApp) advertise(ctx context.Context, disc *routing.RoutingDiscovery, topic string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		ttl, err := disc.Advertise(ctx, topic)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(15 * time.Second):
				continue
			}
		}
		w := ttl - 5*time.Second
		if w < 5*time.Second {
			w = 5 * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(w):
		}
	}
}
func (ca *ChatApp) findPeers(ctx context.Context, disc *routing.RoutingDiscovery, topic string) {
	fc, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	ch, err := disc.FindPeers(fc, topic)
	if err != nil {
		return
	}
	for p := range ch {
		if p.ID == globalHost.ID() || len(p.Addrs) == 0 {
			continue
		}
		if globalHost.Network().Connectedness(p.ID) != network.Connected {
			go func(ai peer.AddrInfo) {
				c, cancel := context.WithTimeout(ctx, 15*time.Second)
				defer cancel()
				_ = globalHost.Connect(c, ai)
			}(p)
		}
	}
}
func (ca *ChatApp) discover(ctx context.Context, disc *routing.RoutingDiscovery, topic string) {
	time.Sleep(2 * time.Second)
	ca.findPeers(ctx, disc, topic)
	tk := time.NewTicker(10 * time.Second)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			ca.findPeers(ctx, disc, topic)
			ca.ping()
			ca.checkOffline()
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Statistics (online peers per day / per month)
// ─────────────────────────────────────────────────────────────────────────────
type StatsData struct {
	Days map[string]int `json:"days"`
}

var (
	statsMu   sync.Mutex
	statsData StatsData
	statsPath string
)

func statsFilePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	full := filepath.Join(dir, "2cup-chat")
	_ = os.MkdirAll(full, 0755)
	return filepath.Join(full, "stats.json")
}

func loadStats() {
	statsPath = statsFilePath()
	statsData = StatsData{Days: make(map[string]int)}
	b, err := os.ReadFile(statsPath)
	if err != nil {
		return
	}
	_ = json.Unmarshal(b, &statsData)
	if statsData.Days == nil {
		statsData.Days = make(map[string]int)
	}
}

func saveStats() {
	b, err := json.MarshalIndent(statsData, "", "  ")
	if err == nil {
		_ = os.WriteFile(statsPath, b, 0644)
	}
}

func recordOnline(count int) {
	statsMu.Lock()
	defer statsMu.Unlock()
	today := time.Now().Format("2006-01-02")
	if count > statsData.Days[today] {
		statsData.Days[today] = count
		saveStats()
	}
}

func todayPeakVal() int {
	statsMu.Lock()
	defer statsMu.Unlock()
	return statsData.Days[time.Now().Format("2006-01-02")]
}

func monthlyPeak(monthPrefix string) int {
	statsMu.Lock()
	defer statsMu.Unlock()
	max := 0
	for d, c := range statsData.Days {
		if len(d) >= 7 && d[:7] == monthPrefix && c > max {
			max = c
		}
	}
	return max
}

func statsHistory(limit int) []struct {
	Day string
	Cnt int
} {
	statsMu.Lock()
	defer statsMu.Unlock()
	type kv struct {
		Day string
		Cnt int
	}
	var list []kv
	for d, c := range statsData.Days {
		list = append(list, kv{d, c})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Day > list[j].Day })
	if len(list) > limit {
		list = list[:limit]
	}
	out := make([]struct {
		Day string
		Cnt int
	}, len(list))
	for i, x := range list {
		out[i].Day = x.Day
		out[i].Cnt = x.Cnt
	}
	return out
}

func showStatsWindow(a fyne.App, ca *ChatApp) {
	sw := a.NewWindow("2cup — Statistics")
	sw.Resize(fyne.NewSize(440, 520))
	sw.CenterOnScreen()

	online := 0
	if globalHost != nil {
		online = len(globalHost.Network().Peers())
	}
	if ca != nil {
		roomOnline := 0
		ca.mu.RLock()
		for _, p := range ca.peers {
			if p.Online {
				roomOnline++
			}
		}
		ca.mu.RUnlock()
		if roomOnline > online {
			online = roomOnline
		}
	}

	tb := titleBar("Statistics")
	nowLbl := ctext95(fmt.Sprintf("Online now: %d peer(s)", online), w95Black, 13, true)
	todayLbl := ctext95(fmt.Sprintf("Today's peak: %d", todayPeakVal()), w95Black, 12, false)
	monthLbl := ctext95(fmt.Sprintf("This month's peak: %d", monthlyPeak(time.Now().Format("2006-01"))), w95Black, 12, false)

	hist := statsHistory(30)
	maxCnt := 1
	for _, x := range hist {
		if x.Cnt > maxCnt {
			maxCnt = x.Cnt
		}
	}
	const barAreaW float32 = 220
	histBox := container.NewVBox()
	for _, x := range hist {
		barW := barAreaW * float32(x.Cnt) / float32(maxCnt)
		if barW < 2 && x.Cnt > 0 {
			barW = 2
		}
		bar := canvas.NewRectangle(w95Green)
		bar.SetMinSize(fyne.NewSize(barW, 12))
		barRow := container.NewHBox(bar)
		row := container.NewBorder(nil, nil,
			ctext95(x.Day, w95Black, 11, false), ctext95(fmt.Sprintf(" %d", x.Cnt), w95DkGray, 11, false),
			container.NewHBox(spacer95(6, 0), barRow))
		histBox.Add(row)
		histBox.Add(spacer95(0, 3))
	}
	if len(hist) == 0 {
		histBox.Add(ctext95("No data yet.", w95DkGray, 11, false))
	}
	histScroll := container.NewScroll(histBox)
	histScroll.SetMinSize(fyne.NewSize(380, 260))

	closeBtn := widget.NewButton("  Close  ", func() { sw.Close() })

	body := container.NewVBox(
		spacer95(0, 6),
		container.NewCenter(ctext95("ONLINE STATISTICS", w95Black, 15, true)),
		spacer95(0, 8),
		container.NewHBox(spacer95(8, 0), nowLbl),
		container.NewHBox(spacer95(8, 0), todayLbl),
		container.NewHBox(spacer95(8, 0), monthLbl),
		spacer95(0, 8),
		hline95(),
		spacer95(0, 6),
		container.NewHBox(spacer95(8, 0), ctext95("History (last 30 days):", w95Black, 12, true)),
		spacer95(0, 4),
		container.NewPadded(sunken3D(histScroll)),
		spacer95(0, 10),
		container.NewCenter(closeBtn),
		spacer95(0, 8),
	)

	outerBg := canvas.NewRectangle(w95Silver)
	outerBg.StrokeColor = w95White
	outerBg.StrokeWidth = 3
	sw.SetContent(container.NewStack(outerBg, container.NewVBox(tb, body)))
	sw.Show()
}

// ─────────────────────────────────────────────────────────────────────────────
// EULA
// ─────────────────────────────────────────────────────────────────────────────
func eulaAccepted() bool {
	dir, _ := os.UserConfigDir()
	_, err := os.Stat(filepath.Join(dir, "2cup-chat", "eula_accepted"))
	return err == nil
}

func saveEulaAcceptance() {
	dir, _ := os.UserConfigDir()
	path := filepath.Join(dir, "2cup-chat", "eula_accepted")
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte("accepted"), 0644)
}

func showEULA(a fyne.App, onAccept func()) {
	w := a.NewWindow("2cup Chat - Terms of Use")
	w.Resize(fyne.NewSize(540, 440))
	w.SetFixedSize(true)
	w.CenterOnScreen()

	tb := titleBar("Terms of Use")
	title := ctext95("PLEASE READ BEFORE YOU CONTINUE", w95Black, 14, true)
	title.Alignment = fyne.TextAlignCenter

	rulesText := `By using 2cup P2P Chat you agree to the following rules:
    
1. Do not use this application for illegal activities.
2. Do not send spam, malware, or harmful content to other peers.
3. Respect other users - no harassment, threats, or abuse.
4. Messages are end-to-end encrypted, but you are responsible
   for what you choose to share.
5. This is peer-to-peer software: your connection may be
   relayed through other peers on the network.
6. The developers are not responsible for any damage or loss
   caused by the use of this software.
7. By clicking "Accept" you confirm you have read and agree
   to these terms.

If you do not agree, click "Cancel" and the application will
close immediately.`

	body := widget.NewLabel(rulesText)
	body.Wrapping = fyne.TextWrapWord
	scroll := container.NewScroll(container.NewPadded(body))
	scroll.SetMinSize(fyne.NewSize(480, 260))

	acceptBtn := widget.NewButton("  Accept  ", func() {
		saveEulaAcceptance()
		w.Close()
		onAccept()
	})
	cancelBtn := widget.NewButton("  Cancel  ", func() { w.Close(); a.Quit() })
	btnRow := container.NewCenter(container.NewHBox(acceptBtn, spacer95(20, 0), cancelBtn))

	body95 := container.NewVBox(
		spacer95(0, 6),
		container.NewCenter(title),
		spacer95(0, 8),
		container.NewPadded(sunken3D(scroll)),
		spacer95(0, 10),
		btnRow,
		spacer95(0, 10),
	)

	w.SetCloseIntercept(func() { w.Close(); a.Quit() })

	outerBg := canvas.NewRectangle(w95Silver)
	outerBg.StrokeColor = w95White
	outerBg.StrokeWidth = 3
	desktopBg := canvas.NewRectangle(w95Desktop)
	windowCard := container.NewStack(outerBg, container.NewVBox(tb, body95))
	w.SetContent(container.NewStack(desktopBg, container.NewCenter(windowCard)))
	w.Show()
}

// ─────────────────────────────────────────────────────────────────────────────
// Diagnostics: "Test Connection"
// ─────────────────────────────────────────────────────────────────────────────
type diagCheck struct {
	Name string
	OK   bool
	Info string
}

func runDiagnostics() []diagCheck {
	var out []diagCheck

	if globalHost == nil {
		out = append(out, diagCheck{"libp2p host", false,
			"Host never started — check app.log for the exact startup error (often: port already in use, or no permission to bind)."})
		return out
	}
	out = append(out, diagCheck{"libp2p host", true, "Running, peer ID " + globalHost.ID().String()[:12] + "…"})

	addrs := globalHost.Addrs()
	if len(addrs) == 0 {
		out = append(out, diagCheck{"Listen addresses", false,
			"No listen addresses at all — Windows Firewall or antivirus may be blocking the app. Try adding an exclusion for 2cup.exe."})
	} else {
		out = append(out, diagCheck{"Listen addresses", true, fmt.Sprintf("%d address(es), e.g. %s", len(addrs), addrs[0].String())})
	}

	peers := globalHost.Network().Peers()
	if len(peers) == 0 {
		out = append(out, diagCheck{"Connected peers", false,
			"0 peers connected — either you have no internet route to the public bootstrap nodes, or a firewall/VPN is blocking outbound TCP/UDP. Both Join Chat and Messages need at least one peer connection to work."})
	} else {
		out = append(out, diagCheck{"Connected peers", true, fmt.Sprintf("%d peer(s) connected", len(peers))})
	}

	if globalDHT == nil {
		out = append(out, diagCheck{"DHT", false, "DHT never initialized — check app.log for the startup error."})
	} else {
		rtSize := globalDHT.RoutingTable().Size()
		if rtSize == 0 {
			out = append(out, diagCheck{"DHT routing table", false,
				"0 entries — the DHT couldn't reach any bootstrap peers, so Join Chat's room lookup and Messages' friend lookup will both fail to find anyone. This is almost always a network/firewall issue, not a bug in the app."})
		} else {
			out = append(out, diagCheck{"DHT routing table", true, fmt.Sprintf("%d entries", rtSize)})
		}

		ctx, cancel := context.WithTimeout(globalCtx, 30*time.Second)
		testID := fmt.Sprintf("selftest-%d", time.Now().UnixNano())
		c, cidErr := userIDCid(testID)
		if cidErr != nil {
			out = append(out, diagCheck{"DHT round-trip", false, "Internal error building test key: " + cidErr.Error()})
		} else if err := globalDHT.Provide(ctx, c, true); err != nil {
			out = append(out, diagCheck{"DHT round-trip (Provide)", false,
				"Provide failed: " + err.Error() + " — this is why friend lookups in Messages and room lookups in Join Chat can fail even when peers show as connected."})
		} else {
			found := false
			for pi := range globalDHT.FindProvidersAsync(ctx, c, 1) {
				if pi.ID == globalHost.ID() {
					found = true
				}
			}
			if found {
				out = append(out, diagCheck{"DHT round-trip", true, "Announce + lookup of a test key succeeded."})
			} else {
				out = append(out, diagCheck{"DHT round-trip", false,
					"Announced a test key but couldn't find it again — DHT writes may not be propagating. Usually resolves once more peers connect; try again in a minute."})
			}
		}
		cancel()
	}

	if globalPS == nil {
		out = append(out, diagCheck{"Pubsub (chat rooms)", false, "GossipSub never initialized — check app.log."})
	} else {
		topic, err := globalPS.Join(fmt.Sprintf("2cup-selftest-%d", time.Now().UnixNano()))
		if err != nil {
			out = append(out, diagCheck{"Pubsub (chat rooms)", false, "Could not join a test topic: " + err.Error()})
		} else {
			out = append(out, diagCheck{"Pubsub (chat rooms)", true, "GossipSub is running normally."})
			_ = topic.Close()
		}
	}

	profile := loadProfile()
	if profile == nil {
		out = append(out, diagCheck{"Messages account", true,
			"No account yet on this device — register from the Messages screen, then run Test Connection again to check the DM listener, key publishing, and message storage."})
		return out
	}
	out = append(out, diagCheck{"Messages account", true, "Logged in as ID " + profile.ID})

	dmMu.Lock()
	listenerUp := dmListenerCancel != nil
	dmMu.Unlock()
	if !listenerUp {
		out = append(out, diagCheck{"DM listener", false,
			"Not running — incoming Messages won't be delivered until you log in (or reopen Messages) so the listener starts."})
	} else {
		out = append(out, diagCheck{"DM listener", true, "Subscribed to your personal dm-" + profile.ID + " topic."})
	}

	if globalDHT == nil {
		out = append(out, diagCheck{"Public key discoverable", false, "DHT not available (see DHT check above)."})
	} else {
		ctx, cancel := context.WithTimeout(globalCtx, 45*time.Second)
		c, cidErr := userIDCid(profile.ID)
		if cidErr != nil {
			out = append(out, diagCheck{"Public key discoverable", false, "Internal error building your ID key: " + cidErr.Error()})
		} else if err := globalDHT.Provide(ctx, c, true); err != nil {
			out = append(out, diagCheck{"Public key discoverable", false,
				"Could not announce your public key: " + err.Error() + " — friends won't be able to add you right now."})
		} else {
			found := false
			for pi := range globalDHT.FindProvidersAsync(ctx, c, 5) {
				if pi.ID == globalHost.ID() {
					found = true
					break
				}
			}
			if found {
				out = append(out, diagCheck{"Public key discoverable", true, "Your ID resolves correctly — friends should be able to add you."})
			} else {
				out = append(out, diagCheck{"Public key discoverable", false,
					"Announced your key but couldn't confirm it in the DHT yet. This can take a minute right after startup, or points to a DHT propagation issue — try again shortly."})
			}
		}
		cancel()
	}

	storageDir := filepath.Dir(mailboxPath())
	if _, err := os.Stat(storageDir); err != nil {
		out = append(out, diagCheck{"Message storage", false, "Could not access local storage folder: " + err.Error()})
	} else {
		testPath := filepath.Join(storageDir, ".2cup-write-test")
		if err := os.WriteFile(testPath, []byte("ok"), 0644); err != nil {
			out = append(out, diagCheck{"Message storage", false,
				"Local storage folder isn't writable: " + err.Error() + " — received Messages and attachments may fail to save."})
		} else {
			os.Remove(testPath)
			out = append(out, diagCheck{"Message storage", true, "Local mailbox/attachment storage is writable."})
		}
	}

	return out
}

func showDiagnosticsDialog(w fyne.Window) {
	progress := dialog.NewCustomWithoutButtons("Testing connection…",
		container.NewCenter(widget.NewProgressBarInfinite()), w)
	progress.Show()
	go func() {
		results := runDiagnostics()
		fyne.Do(func() {
			progress.Hide()
			box := container.NewVBox()
			anyFail := false
			for _, r := range results {
				mark := "✅"
				if !r.OK {
					mark = "❌"
					anyFail = true
				}
				box.Add(ctext95(mark+" "+r.Name, w95Black, 12, true))
				box.Add(ctext95("    "+r.Info, w95DkGray, 11, false))
				box.Add(spacer95(0, 6))
			}
			if !anyFail {
				box.Add(ctext95("Everything checks out — Join Chat and Messages should both work.", w95Black, 12, true))
			}
			scroll := container.NewScroll(box)
			scroll.SetMinSize(fyne.NewSize(460, 380))
			d := dialog.NewCustom("Connection Test Results", "Close", scroll, w)
			d.Show()
		})
	}()
}

// ─────────────────────────────────────────────────────────────────────────────
// Main menu
// ─────────────────────────────────────────────────────────────────────────────
func showMainMenu(w fyne.Window) {
	tb := titleBar("2cup — P2P Secret Chat")
	logoBg := canvas.NewRectangle(w95Yellow)
	logoBg.SetMinSize(fyne.NewSize(0, 52))
	logoMain := ctext95("2CUP CHAT", w95Black, 26, true)
	logoMain.Alignment = fyne.TextAlignCenter
	logoSub := ctext95("Peer-to-Peer * E2E Encrypted * No Server", w95DkGray, 11, false)
	logoSub.Alignment = fyne.TextAlignCenter
	logoArea := container.NewStack(logoBg, container.NewCenter(container.NewVBox(logoMain, logoSub)))

	chatBtn := widget.NewButton("  💬 Join Chat  ", func() { showJoin(w) })
	msgBtn := widget.NewButton("  📩 Messages  ", func() { showMessagesScreen(w) })
	testBtn := widget.NewButton("  🔧 Test Connection  ", func() { showDiagnosticsDialog(w) })
	statsBtn := widget.NewButton("  ⚙️ Statistics  ", func() { showStatsWindow(fyne.CurrentApp(), nil) })
	exitBtn := widget.NewButton("  Exit  ", func() { w.Close() })

	body := container.NewVBox(
		spacer95(0, 20),
		container.NewCenter(ctext95("WELCOME TO 2CUP", w95Black, 16, true)),
		spacer95(0, 20),
		container.NewCenter(chatBtn),
		spacer95(0, 10),
		container.NewCenter(msgBtn),
		spacer95(0, 10),
		container.NewCenter(testBtn),
		spacer95(0, 10),
		container.NewCenter(exitBtn),
		spacer95(0, 10),
		hline95(),
		spacer95(0, 8),
		container.NewCenter(statsBtn),
		spacer95(0, 10),
	)

	outerBg := canvas.NewRectangle(w95Silver)
	outerBg.StrokeColor = w95White
	outerBg.StrokeWidth = 3
	w.SetContent(container.NewStack(canvas.NewRectangle(w95Desktop), container.NewCenter(container.NewStack(outerBg, container.NewVBox(tb, logoArea, body)))))
}

// ─────────────────────────────────────────────────────────────────────────────
// Join room screen (with password field)
// ─────────────────────────────────────────────────────────────────────────────
func showJoin(w fyne.Window) {
	tb := titleBar("2cup — P2P Secret Chat")
	logoBg := canvas.NewRectangle(w95Yellow)
	logoBg.SetMinSize(fyne.NewSize(0, 52))
	logoMain := ctext95("2CUP CHAT", w95Black, 26, true)
	logoMain.Alignment = fyne.TextAlignCenter
	logoSub := ctext95("Peer-to-Peer * E2E Encrypted * No Server", w95DkGray, 11, false)
	logoSub.Alignment = fyne.TextAlignCenter
	logoArea := container.NewStack(logoBg, container.NewCenter(container.NewVBox(logoMain, logoSub)))
	nameLbl := ctext95("Your Name:", w95Black, 12, false)
	nameE := widget.NewEntry()
	nameE.SetPlaceHolder("e.g. Alice")
	nameField := sunken3D(nameE)
	codeLbl := ctext95("Room Code (6 digits):", w95Black, 12, false)
	digits := make([]*widget.Entry, 6)
	dWidgets := make([]fyne.CanvasObject, 6)
	for i := range digits {
		e := widget.NewEntry()
		e.SetPlaceHolder("0")
		digits[i] = e
		dbg := canvas.NewRectangle(w95White)
		dbg.StrokeColor = w95DkGray
		dbg.StrokeWidth = 2
		dWidgets[i] = container.NewStack(dbg, container.NewPadded(e))
	}
	for i := range digits {
		idx := i
		digits[idx].OnChanged = func(s string) {
			if len(s) > 1 {
				rs := []rune(s)
				for j := 0; j < 6 && j < len(rs); j++ {
					if rs[j] >= '0' && rs[j] <= '9' {
						digits[j].SetText(string(rs[j]))
					}
				}
				return
			}
			if len(s) == 1 {
				if s[0] < '0' || s[0] > '9' {
					digits[idx].SetText("")
					return
				}
				if idx < 5 {
					w.Canvas().Focus(digits[idx+1])
				}
			}
		}
	}
	codeRow := container.NewGridWithColumns(6, dWidgets...)

	passLbl := ctext95("Room Password (4-12 chars):", w95Black, 12, false)
	passEntry := widget.NewPasswordEntry()
	passEntry.SetPlaceHolder("Enter password")
	passField := sunken3D(passEntry)

	errLbl := ctext95("", w95Red, 12, false)
	genBtn := widget.NewButton("Random Code", func() {
		code := fmt.Sprintf("%06d", rand.Intn(1000000))
		for i, d := range digits {
			d.SetText(string(code[i]))
		}
	})
	settingsBtn := widget.NewButton("⚙️", func() {
		showStatsWindow(fyne.CurrentApp(), nil)
	})
	msgBtn := widget.NewButton("📩 Messages", func() { showMessagesScreen(w) })
	var joinBtn *widget.Button
	joinBtn = widget.NewButton("  Connect  ", func() {
		code := ""
		for _, d := range digits {
			if d.Text == "" || d.Text[0] < '0' || d.Text[0] > '9' {
				errLbl.Text = "Please fill all 6 digits."
				errLbl.Refresh()
				return
			}
			code += d.Text
		}
		name := nameE.Text
		if len([]rune(name)) > 20 {
			errLbl.Text = "Name too long (max 20 chars)."
			errLbl.Refresh()
			return
		}
		if name == "" {
			name = fmt.Sprintf("User%d", rand.Intn(9000)+1000)
		}
		password := passEntry.Text
		if len([]rune(password)) < 4 || len([]rune(password)) > 12 {
			errLbl.Text = "Password must be 4-12 characters"
			errLbl.Refresh()
			return
		}
		errLbl.Text = "Connecting, please wait..."
		errLbl.Refresh()
		joinBtn.Disable()
		go func() {
			if err := launchChat(w, code, password, name); err != nil {
				fyne.Do(func() {
					errLbl.Text = "Connection failed: " + err.Error()
					errLbl.Refresh()
					joinBtn.Enable()
				})
			}
		}()
	})
	btnBar := container.NewHBox(spacer95(4, 0), genBtn, spacer95(8, 0), joinBtn, spacer95(8, 0), settingsBtn, spacer95(8, 0), msgBtn)
	div := hline95()
	formArea := container.NewVBox(
		spacer95(0, 6),
		container.NewHBox(spacer95(8, 0), nameLbl),
		container.NewPadded(nameField),
		spacer95(0, 4),
		container.NewHBox(spacer95(8, 0), codeLbl),
		container.NewPadded(codeRow),
		spacer95(0, 4),
		container.NewHBox(spacer95(8, 0), passLbl),
		container.NewPadded(passField),
		spacer95(0, 6),
		div,
		spacer95(0, 6),
		container.NewCenter(errLbl),
		container.NewCenter(btnBar),
		spacer95(0, 8),
	)
	outerBg := canvas.NewRectangle(w95Silver)
	outerBg.StrokeColor = w95White
	outerBg.StrokeWidth = 3
	window := container.NewVBox(tb, logoArea, formArea)
	windowCard := container.NewStack(outerBg, window)
	desktopBg := canvas.NewRectangle(w95Desktop)
	w.SetContent(container.NewStack(desktopBg, container.NewCenter(windowCard)))
}

// ─────────────────────────────────────────────────────────────────────────────
// Launch chat (room) with password
// ─────────────────────────────────────────────────────────────────────────────
func launchChat(w fyne.Window, code, password, name string) error {
	topic := "p2p-e2e-room-" + code
	ca := &ChatApp{
		w:        w,
		peers:    make(map[string]*PeerInfo),
		myName:   name,
		roomCode: code,
		eKey:     roomKey(code, password),
	}
	ctx, cancel := context.WithCancel(globalCtx)

	var err error
	ca.roomTopic, err = globalPS.Join(topic)
	if err != nil {
		cancel()
		return fmt.Errorf("could not join room: %w", err)
	}
	ca.roomSub, err = ca.roomTopic.Subscribe()
	if err != nil {
		cancel()
		_ = ca.roomTopic.Close()
		return fmt.Errorf("could not subscribe to room: %w", err)
	}

	registerCleanup(func() {
		cancel()
		if ca.roomSub != nil {
			ca.roomSub.Cancel()
		}
		if ca.roomTopic != nil {
			ca.roomTopic.Close()
		}
	})

	disc := routing.NewRoutingDiscovery(globalDHT)
	go ca.advertise(ctx, disc, topic)
	go ca.reader(ctx)
	go ca.discover(ctx, disc, topic)

	go func() {
		time.Sleep(2 * time.Second)
		ca.publish("join", "")
	}()

	tb := titleBar("2cup — Room #" + code + "  |  " + name)
	ca.statusLbl = widget.NewLabelWithStyle("Connecting...", fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	lockTxt := ctext95("[LOCK] E2E ON", w95Green, 11, true)
	roomTxt := ctext95("Room: #"+code, w95Black, 11, true)
	statusBar := container.NewStack(
		canvas.NewRectangle(w95Silver),
		container.NewPadded(
			container.NewBorder(nil, nil,
				container.NewHBox(spacer95(4, 0), roomTxt, spacer95(12, 0), lockTxt),
				container.NewHBox(ca.statusLbl, spacer95(4, 0)),
			),
		),
	)
	statusOuter := container.NewStack(canvas.NewRectangle(w95Silver), statusBar)
	ca.msgBox = container.NewVBox()
	ca.msgScroll = container.NewScroll(ca.msgBox)
	msgAreaBg := canvas.NewRectangle(w95White)
	msgAreaBg.StrokeColor = w95DkGray
	msgAreaBg.StrokeWidth = 2
	msgArea := container.NewStack(msgAreaBg, container.NewPadded(ca.msgScroll))
	ca.peerBox = container.NewVBox()
	peerScroll := container.NewScroll(ca.peerBox)
	peerTitleBg := canvas.NewRectangle(w95Yellow)
	peerTitleBg.SetMinSize(fyne.NewSize(0, 22))
	peerTitle := ctext95("  ONLINE", w95Black, 12, true)
	peerTitleRow := container.NewStack(peerTitleBg, container.NewPadded(peerTitle))
	peerPanelBg := canvas.NewRectangle(w95Silver)
	peerPanelBg.StrokeColor = w95DkGray
	peerPanelBg.StrokeWidth = 2
	peerPanel := container.NewStack(peerPanelBg,
		container.NewBorder(peerTitleRow, nil, nil, nil, container.NewPadded(peerScroll)),
	)
	ca.input = widget.NewMultiLineEntry()
	ca.input.SetPlaceHolder("Type here... (Enter = Send)")
	ca.input.SetMinRowsVisible(2)
	ca.input.Wrapping = fyne.TextWrapWord
	ca.charLbl = ctext95("0/500", w95DkGray, 10, false)
	ca.input.OnChanged = func(s string) {
		r := []rune(s)
		ca.charLbl.Text = fmt.Sprintf("%d/500", len(r))
		ca.charLbl.Refresh()
		if len(s) > 0 && s[len(s)-1] == '\n' {
			ca.input.SetText(s[:len(s)-1])
			ca.sendText()
		}
	}
	sendBtn := widget.NewButton("[ SEND ]", func() { ca.sendText() })
	imgBtn := widget.NewButton("[IMG]", func() { ca.sendImage() })
	ca.recBtn = widget.NewButton("[MIC]", func() { ca.toggleRecord() })
	settingsBtn := widget.NewButton("⚙️", func() { showStatsWindow(fyne.CurrentApp(), ca) })
	msgBtn := widget.NewButton("📩 Messages", func() { cancel(); showMessagesScreen(w) })
	exitChatBtn := widget.NewButton("[ EXIT CHAT ]", func() { cancel(); showMainMenu(w) })

	toolbarBg := canvas.NewRectangle(w95Silver)
	toolbarBg.StrokeColor = w95DkGray
	toolbarBg.StrokeWidth = 1
	inputBg := canvas.NewRectangle(w95White)
	inputBg.StrokeColor = w95DkGray
	inputBg.StrokeWidth = 2
	btnRow := container.NewHBox(ca.charLbl, spacer95(6, 0), imgBtn, ca.recBtn, spacer95(6, 0), settingsBtn, spacer95(6, 0), msgBtn, spacer95(6, 0), exitChatBtn, spacer95(6, 0), sendBtn)
	inputPanel := container.NewStack(toolbarBg,
		container.NewPadded(
			container.NewBorder(nil,
				container.NewHBox(spacer95(4, 0), btnRow),
				nil, nil,
				container.NewStack(inputBg, container.NewPadded(ca.input)),
			),
		),
	)
	split := container.NewHSplit(msgArea, peerPanel)
	split.SetOffset(0.78)
	chatContent := container.NewBorder(
		container.NewVBox(statusOuter, hline95()),
		container.NewPadded(inputPanel),
		nil, nil,
		container.NewPadded(split),
	)

	fyne.Do(func() {
		w.SetTitle("2cup P2P Chat — #" + code)
		w.SetContent(container.NewStack(
			canvas.NewRectangle(w95Silver),
			container.NewBorder(tb, nil, nil, nil, chatContent),
		))
	})
	ca.sysMsg(fmt.Sprintf("Room #%s opened. E2E active. Share the code!", code))

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
				total := len(globalHost.Network().Peers())
				online := 0
				ca.mu.RLock()
				for _, p := range ca.peers {
					if p.Online {
						online++
					}
				}
				ca.mu.RUnlock()
				recordOnline(online)
				s := "Searching..."
				if online > 0 {
					s = fmt.Sprintf("In chat: %d | Net peers: %d", online, total)
				} else if total > 0 {
					s = fmt.Sprintf("Net peers: %d | Waiting...", total)
				}
				fyne.Do(func() {
					ca.statusLbl.SetText(s)
				})
			}
		}
	}()

	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// ================== MESSAGES MODULE (E2E encrypted) ========================
// ─────────────────────────────────────────────────────────────────────────────

type UserProfile struct {
	Email    string    `json:"email"`
	PassHash string    `json:"passhash"`
	ID       string    `json:"id"`
	PubKey   string    `json:"pubkey"`
	PrivKey  string    `json:"privkey"`
	Contacts []Contact `json:"contacts"`
}
type Contact struct {
	ID     string `json:"id"`
	PubKey string `json:"pubkey"`
	Name   string `json:"name"`
}
type StoredMessage struct {
	FromID   string `json:"from"`
	ToID     string `json:"to"`
	Text     string `json:"text"`
	Kind     string `json:"kind,omitempty"`
	FileName string `json:"filename,omitempty"`
	FilePath string `json:"filepath,omitempty"`
	Time     int64  `json:"time"`
}
type DMBlob struct {
	From string `json:"from"`
	To   string `json:"to"`
	Data string `json:"data"`
}

type DMPayload struct {
	Kind string `json:"k"`
	Text string `json:"t,omitempty"`
	Name string `json:"n,omitempty"`
	Data string `json:"d,omitempty"`
}

const swarmTopicName = "2cup-swarm-storage-v1"
const swarmCacheMax = 1000

type SwarmMsg struct {
	Type         string `json:"type"`
	TargetPubKey string `json:"target_pubkey"`
	DMBlobJSON   string `json:"dm_blob,omitempty"`
	MsgHash      string `json:"msg_hash,omitempty"`
}

var (
	swarmMu    sync.Mutex
	swarmCache = map[string]SwarmMsg{}
)

func swarmMsgHash(dmBlobJSON string) string {
	h := sha256.Sum256([]byte(dmBlobJSON))
	return hex.EncodeToString(h[:])
}

func storeInSwarmCache(sm SwarmMsg) {
	if sm.MsgHash == "" || sm.DMBlobJSON == "" {
		return
	}
	swarmMu.Lock()
	defer swarmMu.Unlock()
	if _, exists := swarmCache[sm.MsgHash]; exists {
		return
	}
	if len(swarmCache) >= swarmCacheMax {
		for k := range swarmCache {
			delete(swarmCache, k)
			break
		}
	}
	swarmCache[sm.MsgHash] = sm
}

func publishSwarmStore(dmBlobJSON, targetPubKey string) error {
	topic, err := getOrJoinTopic(swarmTopicName)
	if err != nil {
		return err
	}
	sm := SwarmMsg{
		Type:         "store",
		TargetPubKey: targetPubKey,
		DMBlobJSON:   dmBlobJSON,
		MsgHash:      swarmMsgHash(dmBlobJSON),
	}
	data, err := json.Marshal(sm)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(globalCtx, 15*time.Second)
	defer cancel()
	return topic.Publish(ctx, data)
}

func publishSwarmFetch(myPubKey string) error {
	topic, err := getOrJoinTopic(swarmTopicName)
	if err != nil {
		return err
	}
	sm := SwarmMsg{Type: "fetch", TargetPubKey: myPubKey}
	data, err := json.Marshal(sm)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(globalCtx, 15*time.Second)
	defer cancel()
	return topic.Publish(ctx, data)
}

var swarmListenerOnce sync.Once

func runSwarmListener() {
	swarmListenerOnce.Do(func() {
		go func() {
			topic, err := getOrJoinTopic(swarmTopicName)
			if err != nil {
				log.Println("swarm listener join error:", err)
				return
			}
			sub, err := topic.Subscribe()
			if err != nil {
				log.Println("swarm listener subscribe error:", err)
				return
			}
			for {
				msg, err := sub.Next(globalCtx)
				if err != nil {
					return
				}
				handleSwarmMessage(msg)
			}
		}()
	})
}

func handleSwarmMessage(msg *pubsub.Message) {
	var sm SwarmMsg
	if err := json.Unmarshal(msg.Data, &sm); err != nil {
		return
	}
	switch sm.Type {
	case "store":
		storeInSwarmCache(sm)

		profile := loadProfile()
		if profile == nil || sm.TargetPubKey == "" || profile.PubKey != sm.TargetPubKey {
			return
		}
		var blob DMBlob
		if err := json.Unmarshal([]byte(sm.DMBlobJSON), &blob); err != nil {
			return
		}
		deliverDMBlob(blob)

	case "fetch":
		if sm.TargetPubKey == "" {
			return
		}
		swarmMu.Lock()
		var hits []SwarmMsg
		for _, cached := range swarmCache {
			if cached.TargetPubKey == sm.TargetPubKey {
				hits = append(hits, cached)
			}
		}
		swarmMu.Unlock()
		if len(hits) == 0 {
			return
		}
		topic, err := getOrJoinTopic(swarmTopicName)
		if err != nil {
			return
		}
		for _, hit := range hits {
			data, err := json.Marshal(hit)
			if err != nil {
				continue
			}
			ctx, cancel := context.WithTimeout(globalCtx, 15*time.Second)
			_ = topic.Publish(ctx, data)
			cancel()
		}
	}
}

const pubkeyProtoID = "/2cup/pubkey/1.0.0"
const dmProtoID = "/2cup/dm/1.0.0" // ADDED: Direct stream protocol for 1:1 DMs

func userIDCid(id string) (cid.Cid, error) {
	sum, err := mh.Sum([]byte("2cup-user-id-"+id), mh.SHA2_256, -1)
	if err != nil {
		return cid.Cid{}, err
	}
	return cid.NewCidV1(cid.Raw, sum), nil
}

var pubkeyHandlerOnce sync.Once

func registerPubKeyHandler() {
	pubkeyHandlerOnce.Do(func() {
		globalHost.SetStreamHandler(pubkeyProtoID, func(s network.Stream) {
			defer s.Close()
			profile := loadProfile()
			if profile == nil {
				return
			}
			_, _ = s.Write([]byte(profile.PubKey))
		})
	})
}

// ADDED: Direct DM Stream Handler
var dmStreamHandlerOnce sync.Once

func registerDMStreamHandler() {
	dmStreamHandlerOnce.Do(func() {
		globalHost.SetStreamHandler(dmProtoID, func(s network.Stream) {
			defer s.Close()
			data, err := io.ReadAll(io.LimitReader(s, 8*1024*1024))
			if err != nil {
				return
			}
			var blob DMBlob
			if err := json.Unmarshal(data, &blob); err != nil {
				return
			}
			deliverDMBlob(blob)
		})
	})
}

// ADDED: Send DM directly via libp2p stream (much more reliable than pubsub for 1:1)
func sendDMDirect(toID string, blobData []byte) error {
	c, err := userIDCid(toID)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(globalCtx, 30*time.Second)
	defer cancel()
	for pi := range globalDHT.FindProvidersAsync(cctx, c, 3) {
		if pi.ID == globalHost.ID() {
			continue
		}
		if globalHost.Network().Connectedness(pi.ID) != network.Connected {
			connCtx, connCancel := context.WithTimeout(cctx, 10*time.Second)
			_ = globalHost.Connect(connCtx, pi)
			connCancel()
		}
		if globalHost.Network().Connectedness(pi.ID) == network.Connected {
			sCtx, sCancel := context.WithTimeout(cctx, 15*time.Second)
			s, err := globalHost.NewStream(sCtx, pi.ID, dmProtoID)
			if err == nil {
				_, err = s.Write(blobData)
				s.Close()
				sCancel()
				if err == nil {
					return nil // Successfully delivered!
				}
			} else {
				sCancel()
			}
		}
	}
	return fmt.Errorf("could not deliver via stream")
}

func profilePath() string {
	dir, _ := os.UserConfigDir()
	full := filepath.Join(dir, "2cup-chat")
	os.MkdirAll(full, 0755)
	return filepath.Join(full, "user_profile.json")
}
func mailboxPath() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "2cup-chat", "mailbox.json")
}

func dmAttachmentsDir() string {
	dir, _ := os.UserConfigDir()
	full := filepath.Join(dir, "2cup-chat", "dm-attachments")
	os.MkdirAll(full, 0755)
	return full
}

func sanitizeFileName(name string) string {
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		name = "file"
	}
	return name
}

func saveDMAttachment(name string, data []byte) (string, error) {
	safe := sanitizeFileName(name)
	fname := fmt.Sprintf("%d_%s", time.Now().UnixNano(), safe)
	path := filepath.Join(dmAttachmentsDir(), fname)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}
	return path, nil
}

const (
	dmMaxImageBytes = 2 * 1024 * 1024
	dmMaxVoiceBytes = 2 * 1024 * 1024
	dmMaxFileBytes  = 2 * 1024 * 1024
)

var (
	profileFileMu sync.Mutex
	mailboxFileMu sync.Mutex
)

func loadProfileLocked() *UserProfile {
	data, err := ioutil.ReadFile(profilePath())
	if err != nil {
		return nil
	}
	var p UserProfile
	json.Unmarshal(data, &p)
	return &p
}
func saveProfileLocked(p *UserProfile) {
	data, _ := json.MarshalIndent(p, "", "  ")
	ioutil.WriteFile(profilePath(), data, 0644)
}
func loadMailboxLocked() []StoredMessage {
	var msgs []StoredMessage
	data, _ := ioutil.ReadFile(mailboxPath())
	json.Unmarshal(data, &msgs)
	return msgs
}
func saveMailboxLocked(msgs []StoredMessage) {
	data, _ := json.MarshalIndent(msgs, "", "  ")
	ioutil.WriteFile(mailboxPath(), data, 0644)
}

func loadProfile() *UserProfile {
	profileFileMu.Lock()
	defer profileFileMu.Unlock()
	return loadProfileLocked()
}
func saveProfile(p *UserProfile) {
	profileFileMu.Lock()
	defer profileFileMu.Unlock()
	saveProfileLocked(p)
}
func loadMailbox() []StoredMessage {
	mailboxFileMu.Lock()
	defer mailboxFileMu.Unlock()
	return loadMailboxLocked()
}
func saveMailbox(msgs []StoredMessage) {
	mailboxFileMu.Lock()
	defer mailboxFileMu.Unlock()
	saveMailboxLocked(msgs)
}

func mutateProfile(fn func(p *UserProfile) bool) *UserProfile {
	profileFileMu.Lock()
	defer profileFileMu.Unlock()
	p := loadProfileLocked()
	if p == nil {
		return nil
	}
	if fn(p) {
		saveProfileLocked(p)
	}
	return p
}

func mutateMailbox(fn func(msgs []StoredMessage) []StoredMessage) {
	mailboxFileMu.Lock()
	defer mailboxFileMu.Unlock()
	msgs := loadMailboxLocked()
	msgs = fn(msgs)
	saveMailboxLocked(msgs)
}

const mailboxDedupWindow = int64(120)

func mailboxHasEquivalent(msgs []StoredMessage, cand StoredMessage) bool {
	for _, m := range msgs {
		if m.FromID != cand.FromID || m.ToID != cand.ToID || m.Kind != cand.Kind {
			continue
		}
		diff := m.Time - cand.Time
		if diff < 0 {
			diff = -diff
		}
		if diff > mailboxDedupWindow {
			continue
		}
		switch cand.Kind {
		case "img", "voice", "file":
			if m.FileName == cand.FileName {
				return true
			}
		default:
			if m.Text == cand.Text {
				return true
			}
		}
	}
	return false
}
func hashPassword(email, password string) string {
	h := sha256.Sum256([]byte(email + ":" + password))
	return hex.EncodeToString(h[:])
}

func generateUserID() string {
	n := rand.Int63n(900000000000) + 100000000000
	return fmt.Sprintf("%012d", n)
}

func generateKeyPair() (pubKey, privKey *[32]byte, err error) {
	return box.GenerateKey(crand.Reader)
}

func encryptE2E(plain []byte, senderPriv, recipientPub *[32]byte) ([]byte, error) {
	var nonce [24]byte
	if _, err := crand.Read(nonce[:]); err != nil {
		return nil, err
	}
	encrypted := box.Seal(nonce[:], plain, &nonce, recipientPub, senderPriv)
	return encrypted, nil
}
func decryptE2E(encrypted []byte, recipientPriv, senderPub *[32]byte) ([]byte, error) {
	if len(encrypted) < 24 {
		return nil, fmt.Errorf("invalid message")
	}
	var nonce [24]byte
	copy(nonce[:], encrypted[:24])
	decrypted, ok := box.Open(nil, encrypted[24:], &nonce, senderPub, recipientPriv)
	if !ok {
		return nil, fmt.Errorf("decryption failed")
	}
	return decrypted, nil
}

const dhtProvideTimeout = 90 * time.Second
const dhtProvideMaxAttempts = 4
const dhtProvideRetryDelay = 5 * time.Second

func dhtProvideOnce(id string) bool {
	c, err := userIDCid(id)
	if err != nil {
		log.Printf("could not derive DHT key for ID %s: %v", id, err)
		return false
	}
	ctx, cancel := context.WithTimeout(globalCtx, dhtProvideTimeout)
	defer cancel()
	if err := globalDHT.Provide(ctx, c, true); err != nil {
		log.Printf("could not announce public key for ID %s to DHT: %v", id, err)
		return false
	}
	log.Printf("public key for ID %s announced on DHT", id)
	return true
}

func publishPubKeyToDHT(pubKey, id string) {
	registerPubKeyHandler()
	if globalDHT == nil {
		return
	}

	ok := false
	for attempt := 1; attempt <= dhtProvideMaxAttempts; attempt++ {
		if dhtProvideOnce(id) {
			ok = true
			break
		}
		if attempt < dhtProvideMaxAttempts {
			select {
			case <-globalCtx.Done():
				return
			case <-time.After(dhtProvideRetryDelay):
			}
		}
	}
	if !ok {
		log.Printf("public key for ID %s still not announced after %d attempts — will keep retrying in the background", id, dhtProvideMaxAttempts)
	}

	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-globalCtx.Done():
				return
			case <-ticker.C:
				dhtProvideOnce(id)
			}
		}
	}()
}

func getPubKeyFromDHT(id string) (string, error) {
	c, err := userIDCid(id)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(globalCtx, 25*time.Second)
	defer cancel()

	var lastErr error
	for pi := range globalDHT.FindProvidersAsync(ctx, c, 5) {
		if pi.ID == globalHost.ID() {
			continue
		}
		cctx, ccancel := context.WithTimeout(ctx, 10*time.Second)
		if err := globalHost.Connect(cctx, pi); err != nil {
			ccancel()
			lastErr = err
			continue
		}
		s, err := globalHost.NewStream(cctx, pi.ID, pubkeyProtoID)
		if err != nil {
			ccancel()
			lastErr = err
			continue
		}
		data, err := io.ReadAll(s)
		s.Close()
		ccancel()
		if err != nil || len(data) == 0 {
			lastErr = err
			continue
		}
		return string(data), nil
	}
	if lastErr != nil {
		return "", fmt.Errorf("could not find key for ID %s: %v", id, lastErr)
	}
	return "", fmt.Errorf("could not find key for ID %s: no provider found", id)
}

func runPersonalListener(myID string) {
	dmMu.Lock()
	if dmListenerCancel != nil {
		dmListenerCancel()
	}
	if dmListenerSub != nil {
		dmListenerSub.Cancel() // ADDED: Cancel old sub explicitly
	}
	dmListenerCtx, dmListenerCancel = context.WithCancel(globalCtx)
	ctx := dmListenerCtx
	dmMu.Unlock()

	profile := loadProfile()
	if profile != nil {
		publishPubKeyToDHT(profile.PubKey, myID)
	}

	topicName := "dm-" + myID
	topic, err := getOrJoinTopic(topicName)
	if err != nil {
		log.Println("personal listener join error:", err)
		return
	}
	sub, err := topic.Subscribe()
	if err != nil {
		log.Println("personal listener subscribe error:", err)
		return
	}

	dmMu.Lock()
	dmListenerSub = sub // ADDED: Save sub globally
	dmMu.Unlock()

	go func() {
		for {
			msg, err := sub.Next(ctx)
			if err != nil {
				return
			}
			handleIncomingDM(msg)
		}
	}()

	runSwarmListener()
	if profile != nil {
		go func() {
			if err := publishSwarmFetch(profile.PubKey); err != nil {
				log.Println("swarm fetch publish error:", err)
			}
		}()
	}
}

func handleIncomingDM(msg *pubsub.Message) {
	var blob DMBlob
	if err := json.Unmarshal(msg.Data, &blob); err != nil {
		return
	}
	deliverDMBlob(blob)
}

func deliverDMBlob(blob DMBlob) {
	profile := loadProfile()
	if profile == nil || profile.ID != blob.To {
		return
	}
	var senderPubKey string
	for _, c := range profile.Contacts {
		if c.ID == blob.From {
			senderPubKey = c.PubKey
			break
		}
	}
	if senderPubKey == "" {
		key, err := getPubKeyFromDHT(blob.From)
		if err != nil {
			log.Printf("could not fetch key to decrypt message from %s: %v", blob.From, err)
			return
		}
		senderPubKey = key
	}

	recipientPrivBytes, _ := base64.StdEncoding.DecodeString(profile.PrivKey)
	senderPubBytes, _ := base64.StdEncoding.DecodeString(senderPubKey)
	var recipientPriv, senderPub [32]byte
	copy(recipientPriv[:], recipientPrivBytes)
	copy(senderPub[:], senderPubBytes)

	encrypted, _ := base64.StdEncoding.DecodeString(blob.Data)
	plain, err := decryptE2E(encrypted, &recipientPriv, &senderPub)
	if err != nil {
		return
	}

	var payload DMPayload
	if err := json.Unmarshal(plain, &payload); err != nil || payload.Kind == "" {
		payload = DMPayload{Kind: "text", Text: string(plain)}
	}

	sm := StoredMessage{
		FromID: blob.From,
		ToID:   blob.To,
		Time:   time.Now().Unix(),
	}
	switch payload.Kind {
	case "img", "voice", "file":
		raw, err := base64.StdEncoding.DecodeString(payload.Data)
		if err != nil {
			log.Printf("bad attachment data from %s: %v", blob.From, err)
			return
		}
		if payload.Kind != "img" {
			raw = decompressData(raw)
		}
		path, err := saveDMAttachment(payload.Name, raw)
		if err != nil {
			log.Printf("could not save attachment from %s: %v", blob.From, err)
			return
		}
		sm.Kind = payload.Kind
		sm.FileName = sanitizeFileName(payload.Name)
		sm.FilePath = path
	default:
		sm.Text = payload.Text
	}

	mutateMailbox(func(msgs []StoredMessage) []StoredMessage {
		if !mailboxHasEquivalent(msgs, sm) {
			msgs = append(msgs, sm)
		}
		return msgs
	})

	mutateProfile(func(p *UserProfile) bool {
		if p.ID != blob.To {
			return false
		}
		if indexOfContact(p, blob.From) >= 0 {
			return false
		}
		p.Contacts = append(p.Contacts, Contact{
			ID:     blob.From,
			PubKey: senderPubKey,
			Name:   "Friend " + blob.From[:4],
		})
		return true
	})
	log.Printf("incoming direct message (%s) from %s", payload.Kind, blob.From)
}

func ensureConnectedToPeer(ctx context.Context, id string) bool {
	if globalDHT == nil || globalHost == nil {
		return false
	}
	c, err := userIDCid(id)
	if err != nil {
		return false
	}
	cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	for pi := range globalDHT.FindProvidersAsync(cctx, c, 3) {
		if pi.ID == globalHost.ID() {
			continue
		}
		if globalHost.Network().Connectedness(pi.ID) != network.Connected {
			cctx2, ccancel := context.WithTimeout(cctx, 8*time.Second)
			_ = globalHost.Connect(cctx2, pi)
			ccancel()
		}
		connected := globalHost.Network().Connectedness(pi.ID) == network.Connected
		if connected && globalConnMgr != nil {
			globalConnMgr.Protect(pi.ID, "contact")
		}
		return connected
	}
	return false
}

func publishDM(fromID, toID string, payload DMPayload) error {
	profile := loadProfile()
	if profile == nil {
		return fmt.Errorf("no profile")
	}
	var toPubKey string
	peerOnline := false
	for _, c := range profile.Contacts {
		if c.ID == toID {
			toPubKey = c.PubKey
			break
		}
	}
	if toPubKey == "" {
		key, err := getPubKeyFromDHT(toID)
		if err != nil {
			return fmt.Errorf("could not find key for ID %s", toID)
		}
		toPubKey = key
		peerOnline = true
	} else {
		ctx, cancel := context.WithTimeout(globalCtx, 45*time.Second)
		peerOnline = ensureConnectedToPeer(ctx, toID)
		cancel()
	}

	fromPrivBytes, _ := base64.StdEncoding.DecodeString(profile.PrivKey)
	toPubBytes, _ := base64.StdEncoding.DecodeString(toPubKey)
	var fromPriv, toPub [32]byte
	copy(fromPriv[:], fromPrivBytes)
	copy(toPub[:], toPubBytes)

	plain, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	encrypted, err := encryptE2E(plain, &fromPriv, &toPub)
	if err != nil {
		return err
	}
	blob := DMBlob{
		From: fromID,
		To:   toID,
		Data: base64.StdEncoding.EncodeToString(encrypted),
	}
	data, _ := json.Marshal(blob)

	// 1) Try direct stream first (most reliable)
	if err := sendDMDirect(toID, data); err == nil {
		return nil
	}

	if !peerOnline {
		_ = publishSwarmStore(string(data), toPubKey)
		return nil
	}

	// 2) Fallback to pubsub
	topicName := "dm-" + toID
	topic, err := getOrJoinTopic(topicName)
	if err == nil {
		time.Sleep(2 * time.Second) // Give GossipSub mesh time to form
		for attempt := 1; attempt <= 2; attempt++ {
			ctx, cancel := context.WithTimeout(globalCtx, 10*time.Second)
			pErr := topic.Publish(ctx, data)
			cancel()
			if pErr == nil {
				return nil
			}
			time.Sleep(2 * time.Second)
		}
	}

	// 3) Last resort: swarm relay
	_ = publishSwarmStore(string(data), toPubKey)
	return nil
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

var (
	contactOnlineMu     sync.RWMutex
	contactOnlineStatus = map[string]bool{}
	contactsPollCancel  context.CancelFunc
	contactsPollMu      sync.Mutex
)

func checkContactOnline(ctx context.Context, id string) bool {
	c, err := userIDCid(id)
	if err != nil || globalDHT == nil || globalHost == nil {
		return false
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	for pi := range globalDHT.FindProvidersAsync(cctx, c, 1) {
		if pi.ID == globalHost.ID() {
			continue
		}
		if globalHost.Network().Connectedness(pi.ID) != network.Connected {
			cctx2, ccancel := context.WithTimeout(cctx, 10*time.Second)
			_ = globalHost.Connect(cctx2, pi)
			ccancel()
		}
		if globalConnMgr != nil {
			globalConnMgr.Protect(pi.ID, "contact")
		}
		return true
	}
	return false
}

func startContactsPoller(profile *UserProfile, onUpdate func()) {
	contactsPollMu.Lock()
	if contactsPollCancel != nil {
		contactsPollCancel()
	}
	ctx, cancel := context.WithCancel(globalCtx)
	contactsPollCancel = cancel
	contactsPollMu.Unlock()

	go func() {
		tk := time.NewTicker(10 * time.Second)
		defer tk.Stop()
		poll := func() {
			var wg sync.WaitGroup
			for _, c := range profile.Contacts {
				wg.Add(1)
				go func(id string) {
					defer wg.Done()
					online := checkContactOnline(ctx, id)
					contactOnlineMu.Lock()
					contactOnlineStatus[id] = online
					contactOnlineMu.Unlock()
				}(c.ID)
			}
			wg.Wait()
			select {
			case <-ctx.Done():
			default:
				fyne.Do(onUpdate)
			}
		}
		poll()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				poll()
			}
		}
	}()
}

func isContactOnline(id string) bool {
	contactOnlineMu.RLock()
	defer contactOnlineMu.RUnlock()
	return contactOnlineStatus[id]
}

func showMessagesScreen(w fyne.Window) {
	profile := loadProfile()
	if profile == nil {
		showLoginScreen(w)
	} else {
		showContactsScreen(w, profile)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Messages: Login / Register
// ─────────────────────────────────────────────────────────────────────────────
func showLoginScreen(w fyne.Window) {
	tb := titleBar("Messages — Login / Register")

	emailLbl := ctext95("Email:", w95Black, 12, false)
	emailEntry := widget.NewEntry()
	emailEntry.SetPlaceHolder("you@example.com")
	passLbl := ctext95("Password:", w95Black, 12, false)
	passEntry := widget.NewPasswordEntry()
	passEntry.SetPlaceHolder("Password")

	statusLbl := ctext95("", w95Red, 12, false)
	statusLbl.Alignment = fyne.TextAlignCenter

	loginBtn := widget.NewButton("  Login  ", func() {
		email := strings.TrimSpace(emailEntry.Text)
		pass := passEntry.Text
		if email == "" || pass == "" {
			statusLbl.Text = "Please fill in both fields."
			statusLbl.Refresh()
			return
		}
		profile := loadProfile()
		if profile == nil || profile.Email != email || profile.PassHash != hashPassword(email, pass) {
			statusLbl.Text = "Wrong email or password."
			statusLbl.Refresh()
			return
		}
		runPersonalListener(profile.ID)
		showContactsScreen(w, profile)
	})
	regBtn := widget.NewButton("  Register  ", func() {
		email := strings.TrimSpace(emailEntry.Text)
		pass := passEntry.Text
		if email == "" || pass == "" {
			statusLbl.Text = "Please fill in both fields."
			statusLbl.Refresh()
			return
		}
		if len([]rune(pass)) < 4 {
			statusLbl.Text = "Password should be at least 4 characters."
			statusLbl.Refresh()
			return
		}
		if loadProfile() != nil {
			statusLbl.Text = "A profile already exists on this device (logout first)."
			statusLbl.Refresh()
			return
		}
		id := generateUserID()
		pub, priv, err := generateKeyPair()
		if err != nil {
			statusLbl.Text = "Key generation failed."
			statusLbl.Refresh()
			return
		}
		profile := &UserProfile{
			Email:    email,
			PassHash: hashPassword(email, pass),
			ID:       id,
			PubKey:   base64.StdEncoding.EncodeToString(pub[:]),
			PrivKey:  base64.StdEncoding.EncodeToString(priv[:]),
		}
		saveProfile(profile)
		runPersonalListener(profile.ID)
		dialog.ShowInformation(
			"Account created",
			"Your new ID is:\n\n"+id+"\n\nShare this ID with friends so they can add you.",
			w,
		)
		showContactsScreen(w, profile)
	})
	backBtn := widget.NewButton("  Back  ", func() { showMainMenu(w) })

	form := container.NewVBox(
		spacer95(0, 10),
		container.NewHBox(spacer95(8, 0), emailLbl),
		container.NewPadded(sunken3D(emailEntry)),
		spacer95(0, 6),
		container.NewHBox(spacer95(8, 0), passLbl),
		container.NewPadded(sunken3D(passEntry)),
		spacer95(0, 8),
		container.NewCenter(statusLbl),
		spacer95(0, 6),
		container.NewCenter(container.NewHBox(loginBtn, spacer95(10, 0), regBtn, spacer95(10, 0), backBtn)),
		spacer95(0, 10),
	)

	outerBg := canvas.NewRectangle(w95Silver)
	outerBg.StrokeColor = w95White
	outerBg.StrokeWidth = 3
	desktopBg := canvas.NewRectangle(w95Desktop)
	w.SetContent(container.NewStack(desktopBg, container.NewCenter(container.NewStack(outerBg, container.NewVBox(tb, form)))))
}

// ─────────────────────────────────────────────────────────────────────────────
// Messages: single-screen Messenger UI
// ─────────────────────────────────────────────────────────────────────────────
func showContactsScreen(w fyne.Window, profile *UserProfile) {
	tb := titleBar("Messages")

	idLbl := ctext95("YOUR ID (share this with friends):", w95Black, 12, true)
	idEntry := widget.NewEntry()
	idEntry.SetText(profile.ID)
	idEntry.Disable()
	copyBtn := widget.NewButton("Copy", func() {
		w.Clipboard().SetContent(profile.ID)
		dialog.ShowInformation("Copied", "Your ID has been copied to the clipboard.", w)
	})
	idRow := container.NewBorder(nil, nil, nil, copyBtn, sunken3D(idEntry))

	currentContactID := ""
	getCurrentContact := func() *Contact {
		idx := indexOfContact(profile, currentContactID)
		if idx < 0 {
			return nil
		}
		return &profile.Contacts[idx]
	}

	chatTitleLbl := ctext95("Select a friend to start chatting", w95Black, 13, true)
	chatBox := container.NewVBox()
	chatScroll := container.NewScroll(chatBox)
	chatScrollBg := canvas.NewRectangle(w95White)
	chatScrollBg.StrokeColor = w95DkGray
	chatScrollBg.StrokeWidth = 2
	chatArea := container.NewStack(chatScrollBg, container.NewPadded(chatScroll))

	var lastRenderedCount int
	renderMessages := func() {
		chatBox.RemoveAll()
		cc := getCurrentContact()
		if cc == nil {
			chatBox.Add(ctext95("No friend selected.", w95DkGray, 11, false))
			chatBox.Refresh()
			return
		}
		all := loadMailbox()
		count := 0
		for _, m := range all {
			var who string
			var clr color.Color
			if m.ToID == profile.ID && m.FromID == cc.ID {
				who, clr = cc.Name, w95TitleBar
			} else if m.FromID == profile.ID && m.ToID == cc.ID {
				who, clr = "Me", w95Black
			} else {
				continue
			}
			ts := time.Unix(m.Time, 0).Format("15:04")
			header := ctext95(ts+"  "+who+":", clr, 11, false)

			var body fyne.CanvasObject
			switch m.Kind {
			case "img":
				if data, rerr := os.ReadFile(m.FilePath); rerr == nil {
					img := canvas.NewImageFromReader(bytes.NewReader(data), m.FileName)
					img.FillMode = canvas.ImageFillContain
					img.SetMinSize(fyne.NewSize(200, 140))
					body = img
				} else {
					body = ctext95("[photo unavailable]", w95Red, 11, false)
				}
			case "voice":
				path := m.FilePath
				dur := ""
				if fi, ferr := os.Stat(path); ferr == nil {
					dur = fmt.Sprintf(" [%ds]", int(fi.Size()/32000))
				}
				body = widget.NewButton("► Play voice note"+dur, func() { playWAV(path) })
			case "file":
				path, name := m.FilePath, m.FileName
				szStr := ""
				if fi, ferr := os.Stat(path); ferr == nil {
					szStr = fmt.Sprintf(" (%.1f MB)", float64(fi.Size())/1024/1024)
				}
				body = widget.NewButton("💾 Save "+name+szStr, func() {
					fsd := dialog.NewFileSave(func(wc fyne.URIWriteCloser, ferr error) {
						if ferr != nil || wc == nil {
							return
						}
						defer wc.Close()
						if data, rerr := os.ReadFile(path); rerr == nil {
							wc.Write(data)
						}
					}, w)
					fsd.SetFileName(name)
					fsd.Show()
				})
			default:
				body = ctext95(m.Text, clr, 11, false)
			}
			chatBox.Add(header)
			chatBox.Add(body)
			chatBox.Add(spacer95(0, 4))
			count++
		}
		if count == 0 {
			chatBox.Add(ctext95("No messages yet — say hello!", w95DkGray, 11, false))
		}
		chatBox.Refresh()
		if count != lastRenderedCount {
			chatScroll.ScrollToBottom()
		}
		lastRenderedCount = count
	}

	msgInput := widget.NewEntry()
	msgInput.SetPlaceHolder("Type a message...")
	chatStatusLbl := ctext95("", w95Red, 11, false)

	sendMsg := func() {
		cc := getCurrentContact()
		if cc == nil {
			return
		}
		text := strings.TrimSpace(msgInput.Text)
		if text == "" {
			return
		}
		msgInput.SetText("")

		// ADDED: Save locally first, so message is never lost if network fails
		mutateMailbox(func(msgs []StoredMessage) []StoredMessage {
			return append(msgs, StoredMessage{
				FromID: profile.ID,
				ToID:   cc.ID,
				Text:   text,
				Time:   time.Now().Unix(),
			})
		})
		renderMessages()

		chatStatusLbl.Text = "Sending…"
		chatStatusLbl.Refresh()
		go func() {
			err := publishDM(profile.ID, cc.ID, DMPayload{Kind: "text", Text: text})
			fyne.Do(func() {
				if err != nil {
					chatStatusLbl.Text = "⚠️ Saved, delivery pending: " + err.Error()
				} else {
					chatStatusLbl.Text = ""
				}
				chatStatusLbl.Refresh()
			})
		}()
	}
	msgInput.OnSubmitted = func(string) { sendMsg() }
	sendBtn := widget.NewButton("  Send  ", sendMsg)

	sendPhotoDM := func() {
		cc := getCurrentContact()
		if cc == nil {
			dialog.ShowInformation("No friend selected", "Select a friend from the list first.", w)
			return
		}
		fd := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
			if err != nil || r == nil {
				return
			}
			defer r.Close()
			name := r.URI().Name()
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, r); err != nil {
				chatStatusLbl.Text = "Could not read photo."
				chatStatusLbl.Refresh()
				return
			}
			imgBytes := compressImage(buf.Bytes())
			if len(imgBytes) > dmMaxImageBytes {
				dialog.ShowInformation("Photo too large", "Please pick a smaller photo (max ~2 MB after compression).", w)
				return
			}

			// ADDED: Save locally first
			if path, serr := saveDMAttachment(name, imgBytes); serr == nil {
				mutateMailbox(func(msgs []StoredMessage) []StoredMessage {
					return append(msgs, StoredMessage{
						FromID: profile.ID, ToID: cc.ID, Kind: "img",
						FileName: sanitizeFileName(name), FilePath: path, Time: time.Now().Unix(),
					})
				})
				renderMessages()
			}

			chatStatusLbl.Text = "Sending photo…"
			chatStatusLbl.Refresh()
			go func() {
				payload := DMPayload{Kind: "img", Name: name, Data: base64.StdEncoding.EncodeToString(imgBytes)}
				err := publishDM(profile.ID, cc.ID, payload)
				fyne.Do(func() {
					if err != nil {
						chatStatusLbl.Text = "⚠️ Saved, delivery pending: " + err.Error()
					} else {
						chatStatusLbl.Text = ""
					}
					chatStatusLbl.Refresh()
				})
			}()
		}, w)
		fd.SetFilter(storage.NewExtensionFileFilter([]string{".png", ".jpg", ".jpeg", ".gif", ".webp"}))
		fd.Show()
	}
	photoBtn := widget.NewButton("📷", sendPhotoDM)

	sendFileDM := func() {
		cc := getCurrentContact()
		if cc == nil {
			dialog.ShowInformation("No friend selected", "Select a friend from the list first.", w)
			return
		}
		fd := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
			if err != nil || r == nil {
				return
			}
			defer r.Close()
			name := r.URI().Name()
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, r); err != nil {
				chatStatusLbl.Text = "Could not read file."
				chatStatusLbl.Refresh()
				return
			}
			raw := buf.Bytes()
			compressed := compressData(raw)
			if len(compressed) > dmMaxFileBytes {
				dialog.ShowInformation("File too large",
					fmt.Sprintf("That file is too big to send this way (max ~%d MB after compression). Large videos in particular may need to be trimmed or shared another way.", dmMaxFileBytes/(1024*1024)), w)
				return
			}

			// ADDED: Save locally first
			if path, serr := saveDMAttachment(name, raw); serr == nil {
				mutateMailbox(func(msgs []StoredMessage) []StoredMessage {
					return append(msgs, StoredMessage{
						FromID: profile.ID, ToID: cc.ID, Kind: "file",
						FileName: sanitizeFileName(name), FilePath: path, Time: time.Now().Unix(),
					})
				})
				renderMessages()
			}

			chatStatusLbl.Text = "Sending " + name + "…"
			chatStatusLbl.Refresh()
			go func() {
				payload := DMPayload{Kind: "file", Name: name, Data: base64.StdEncoding.EncodeToString(compressed)}
				err := publishDM(profile.ID, cc.ID, payload)
				fyne.Do(func() {
					if err != nil {
						chatStatusLbl.Text = "⚠️ Saved, delivery pending: " + err.Error()
					} else {
						chatStatusLbl.Text = ""
					}
					chatStatusLbl.Refresh()
				})
			}()
		}, w)
		fd.SetFilter(storage.NewExtensionFileFilter([]string{".mp4", ".mov", ".avi", ".mkv", ".webm", ".pdf", ".zip"}))
		fd.Show()
	}
	fileBtn := widget.NewButton("📎", sendFileDM)

	dmRecording := false
	var dmRecCtx context.Context
	var dmRecCancel context.CancelFunc
	var voiceBtn *widget.Button

	finishAndSendVoiceDM := func() {
		stopRecord()
		b, err := os.ReadFile(voiceFile)
		if err != nil || len(b) == 0 {
			fyne.Do(func() {
				dmRecording = false
				voiceBtn.SetText("🎤")
				chatStatusLbl.Text = "No audio recorded."
				chatStatusLbl.Refresh()
			})
			return
		}
		compressed := compressData(b)
		if len(compressed) > dmMaxVoiceBytes {
			fyne.Do(func() {
				dmRecording = false
				voiceBtn.SetText("🎤")
				chatStatusLbl.Text = "Recording too long — keep it under ~30s."
				chatStatusLbl.Refresh()
			})
			return
		}
		cc := getCurrentContact()
		if cc == nil {
			fyne.Do(func() { dmRecording = false; voiceBtn.SetText("🎤") })
			return
		}

		// ADDED: Save locally first
		if path, serr := saveDMAttachment("voice.wav", b); serr == nil {
			mutateMailbox(func(msgs []StoredMessage) []StoredMessage {
				return append(msgs, StoredMessage{
					FromID: profile.ID, ToID: cc.ID, Kind: "voice",
					FileName: "voice.wav", FilePath: path, Time: time.Now().Unix(),
				})
			})
		}
		fyne.Do(func() {
			dmRecording = false
			voiceBtn.SetText("🎤")
			renderMessages()
		})

		payload := DMPayload{Kind: "voice", Name: "voice.wav", Data: base64.StdEncoding.EncodeToString(compressed)}
		err = publishDM(profile.ID, cc.ID, payload)
		fyne.Do(func() {
			if err != nil {
				chatStatusLbl.Text = "⚠️ Saved, delivery pending: " + err.Error()
			} else {
				chatStatusLbl.Text = ""
			}
			chatStatusLbl.Refresh()
		})
	}
	toggleVoiceDM := func() {
		if !dmRecording {
			cc := getCurrentContact()
			if cc == nil {
				dialog.ShowInformation("No friend selected", "Select a friend from the list first.", w)
				return
			}
			dmRecording = true
			dmRecCtx, dmRecCancel = context.WithCancel(context.Background())
			startRecord()
			voiceBtn.SetText("⏹")
			go func() {
				select {
				case <-dmRecCtx.Done():
					return
				case <-time.After(30 * time.Second):
					finishAndSendVoiceDM()
				}
			}()
		} else {
			if dmRecCancel != nil {
				dmRecCancel()
			}
			go finishAndSendVoiceDM()
		}
	}
	voiceBtn = widget.NewButton("🎤", toggleVoiceDM)

	const (
		filterAll = iota
		filterOnline
		filterOffline
	)
	currentFilter := filterAll
	var visibleIdx []int

	var contactList *widget.List
	rebuildVisible := func() {
		visibleIdx = visibleIdx[:0]
		for i, c := range profile.Contacts {
			online := isContactOnline(c.ID)
			switch currentFilter {
			case filterOnline:
				if !online {
					continue
				}
			case filterOffline:
				if online {
					continue
				}
			}
			visibleIdx = append(visibleIdx, i)
		}
	}

	selectContact := func(idx int) {
		if idx < 0 || idx >= len(profile.Contacts) {
			currentContactID = ""
			chatTitleLbl.Text = "Select a friend to start chatting"
		} else {
			c := profile.Contacts[idx]
			currentContactID = c.ID
			dot := "🔴"
			if isContactOnline(c.ID) {
				dot = "🟢"
			}
			chatTitleLbl.Text = dot + "  Chat with " + c.Name + "  (ID: " + c.ID + ")"
		}
		chatTitleLbl.Refresh()
		lastRenderedCount = -1
		renderMessages()
	}

	contactList = widget.NewList(
		func() int { return len(visibleIdx) },
		func() fyne.CanvasObject { return widget.NewLabel("template") },
		func(i int, o fyne.CanvasObject) {
			c := profile.Contacts[visibleIdx[i]]
			dot := "🔴"
			if isContactOnline(c.ID) {
				dot = "🟢"
			}
			o.(*widget.Label).SetText(fmt.Sprintf("%s  %s   (ID: %s)", dot, c.Name, c.ID))
		},
	)
	contactList.OnSelected = func(i int) {
		if i < 0 || i >= len(visibleIdx) {
			return
		}
		selectContact(visibleIdx[i])
	}
	listBg := canvas.NewRectangle(w95White)
	listBg.StrokeColor = w95DkGray
	listBg.StrokeWidth = 2
	listArea := container.NewStack(listBg, container.NewPadded(contactList))

	emptyHint := ctext95("No friends yet — add one by ID above.", w95DkGray, 11, false)
	emptyCenter := container.NewCenter(emptyHint)
	sidebarListStack := container.NewStack(listArea, emptyCenter)

	refreshSidebar := func() {
		rebuildVisible()
		contactList.Refresh()
		if len(profile.Contacts) == 0 {
			listArea.Hide()
			emptyCenter.Show()
		} else {
			emptyCenter.Hide()
			listArea.Show()
		}
	}
	rebuildVisible()
	refreshSidebar()

	filterGroup := widget.NewRadioGroup([]string{"All", "Online", "Offline"}, func(sel string) {
		switch sel {
		case "Online":
			currentFilter = filterOnline
		case "Offline":
			currentFilter = filterOffline
		default:
			currentFilter = filterAll
		}
		refreshSidebar()
	})
	filterGroup.Horizontal = true
	filterGroup.SetSelected("All")

	addIDEntry := widget.NewEntry()
	addIDEntry.SetPlaceHolder("Friend's 12-digit ID")
	addStatusLbl := ctext95("", w95Red, 11, false)
	var addBtn *widget.Button
	addBtn = widget.NewButton("  Add Friend  ", func() {
		id := strings.TrimSpace(addIDEntry.Text)
		if len(id) != 12 || !isAllDigits(id) {
			dialog.ShowInformation("Invalid ID", "An ID must be exactly 12 digits.", w)
			return
		}
		if id == profile.ID {
			dialog.ShowInformation("Invalid ID", "You can't add yourself as a friend.", w)
			return
		}
		for _, c := range profile.Contacts {
			if c.ID == id {
				dialog.ShowInformation("Already added", "This friend is already in your list.", w)
				return
			}
		}
		addBtn.Disable()
		addStatusLbl.Text = "Looking up " + id + " on the network…"
		addStatusLbl.Refresh()
		go func() {
			pubKey, err := getPubKeyFromDHT(id)
			fyne.Do(func() {
				addBtn.Enable()
				if err != nil {
					addStatusLbl.Text = ""
					addStatusLbl.Refresh()
					dialog.ShowInformation("Could not add friend",
						"Failed to find that user's public key on the network.\nMake sure the ID is correct and your friend has the app open.", w)
					return
				}
				fresh := mutateProfile(func(p *UserProfile) bool {
					if indexOfContact(p, id) >= 0 {
						return false
					}
					p.Contacts = append(p.Contacts, Contact{
						ID:     id,
						PubKey: pubKey,
						Name:   "Friend " + id[:4],
					})
					return true
				})
				if fresh != nil {
					profile.Contacts = fresh.Contacts
				}
				addIDEntry.SetText("")
				addStatusLbl.Text = ""
				addStatusLbl.Refresh()
				refreshSidebar()
				selectContact(indexOfContact(profile, id))
			})
		}()
	})
	addRow := container.NewBorder(nil, nil, nil, addBtn, sunken3D(addIDEntry))

	renameBtn := widget.NewButton("  Rename  ", func() {
		cc := getCurrentContact()
		if cc == nil {
			dialog.ShowInformation("No friend selected", "Select a friend from the list first.", w)
			return
		}
		nameEntry := widget.NewEntry()
		nameEntry.SetText(cc.Name)
		dialog.ShowForm("Rename friend", "Save", "Cancel",
			[]*widget.FormItem{widget.NewFormItem("Name", nameEntry)},
			func(ok bool) {
				if !ok {
					return
				}
				newName := strings.TrimSpace(nameEntry.Text)
				if newName == "" {
					return
				}
				id := cc.ID
				fresh := mutateProfile(func(p *UserProfile) bool {
					idx := indexOfContact(p, id)
					if idx < 0 {
						return false
					}
					p.Contacts[idx].Name = newName
					return true
				})
				if fresh != nil {
					profile.Contacts = fresh.Contacts
				}
				refreshSidebar()
				selectContact(indexOfContact(profile, id))
			}, w)
	})
	deleteBtn := widget.NewButton("  Delete  ", func() {
		cc := getCurrentContact()
		if cc == nil {
			dialog.ShowInformation("No friend selected", "Select a friend from the list first.", w)
			return
		}
		id := cc.ID
		name := cc.Name
		dialog.ShowConfirm("Remove friend", "Remove "+name+" from your friends list?", func(ok bool) {
			if !ok {
				return
			}
			fresh := mutateProfile(func(p *UserProfile) bool {
				idx := indexOfContact(p, id)
				if idx < 0 {
					return false
				}
				p.Contacts = append(p.Contacts[:idx], p.Contacts[idx+1:]...)
				return true
			})
			if fresh != nil {
				profile.Contacts = fresh.Contacts
			}
			selectContact(-1)
			refreshSidebar()
		}, w)
	})
	logoutBtn := widget.NewButton("  Logout  ", func() {
		dialog.ShowConfirm("Logout", "Log out of Messages on this device?", func(ok bool) {
			if !ok {
				return
			}
			dmMu.Lock()
			if dmListenerCancel != nil {
				dmListenerCancel()
				dmListenerCancel = nil
			}
			if dmListenerSub != nil { // ADDED: Cancel sub on logout
				dmListenerSub.Cancel()
				dmListenerSub = nil
			}
			dmMu.Unlock()
			contactsPollMu.Lock()
			if contactsPollCancel != nil {
				contactsPollCancel()
				contactsPollCancel = nil
			}
			contactsPollMu.Unlock()
			os.Remove(profilePath())
			showMainMenu(w)
		}, w)
	})
	backBtn := widget.NewButton("  Back  ", func() {
		contactsPollMu.Lock()
		if contactsPollCancel != nil {
			contactsPollCancel()
			contactsPollCancel = nil
		}
		contactsPollMu.Unlock()
		showMainMenu(w)
	})

	sidebar := container.NewBorder(
		container.NewVBox(
			container.NewHBox(spacer95(4, 0), ctext95("Friends:", w95Black, 12, true)),
			container.NewCenter(filterGroup),
			spacer95(0, 4),
		),
		container.NewVBox(
			spacer95(0, 4),
			container.NewCenter(container.NewHBox(renameBtn, spacer95(4, 0), deleteBtn)),
			spacer95(0, 4),
		),
		nil, nil,
		sidebarListStack,
	)

	inputBg := canvas.NewRectangle(w95White)
	inputBg.StrokeColor = w95DkGray
	inputBg.StrokeWidth = 2
	attachRow := container.NewHBox(photoBtn, fileBtn, voiceBtn, sendBtn)
	inputRow := container.NewBorder(nil, nil, nil, attachRow,
		container.NewStack(inputBg, container.NewPadded(msgInput)))

	chatPane := container.NewBorder(
		container.NewVBox(container.NewHBox(spacer95(4, 0), chatTitleLbl), spacer95(0, 4)),
		container.NewVBox(container.NewCenter(chatStatusLbl), container.NewPadded(inputRow)),
		nil, nil,
		container.NewPadded(chatArea),
	)

	split := container.NewHSplit(chatPane, sidebar)
	split.SetOffset(0.66)

	top := container.NewVBox(
		tb,
		spacer95(0, 6),
		container.NewHBox(spacer95(8, 0), idLbl),
		container.NewPadded(idRow),
		spacer95(0, 4),
		container.NewHBox(spacer95(8, 0), ctext95("Add a friend by ID:", w95Black, 12, true)),
		container.NewPadded(addRow),
		container.NewCenter(addStatusLbl),
		hline95(),
	)
	bottom := container.NewVBox(
		spacer95(0, 4),
		container.NewCenter(container.NewHBox(logoutBtn, spacer95(8, 0), backBtn)),
		spacer95(0, 6),
	)

	content := container.NewBorder(top, bottom, nil, nil, container.NewPadded(split))
	w.SetContent(container.NewStack(canvas.NewRectangle(w95Desktop), container.NewPadded(content)))

	renderMessages()
	startContactsPoller(profile, refreshSidebar)

	msgCtx, msgCancel := context.WithCancel(globalCtx)
	registerCleanup(func() { msgCancel() })
	go func() {
		tk := time.NewTicker(2 * time.Second)
		defer tk.Stop()
		for {
			select {
			case <-msgCtx.Done():
				return
			case <-tk.C:
				fresh := loadProfile()
				fyne.Do(func() {
					if fresh != nil && len(fresh.Contacts) != len(profile.Contacts) {
						profile.Contacts = fresh.Contacts
						refreshSidebar()
					}
					renderMessages()
				})
			}
		}
	}()
}

func indexOfContact(profile *UserProfile, id string) int {
	for i, c := range profile.Contacts {
		if c.ID == id {
			return i
		}
	}
	return -1
}

// ─────────────────────────────────────────────────────────────────────────────
// Автозагрузка в Windows с диалогом подтверждения
// ─────────────────────────────────────────────────────────────────────────────
const startupRegKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const startupValueName = "2cup"

func isInStartup(exePath string) bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, startupRegKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	val, _, err := key.GetStringValue(startupValueName)
	if err != nil {
		return false
	}
	return val == startupCommand(exePath)
}

func startupCommand(exePath string) string {
	return `"` + exePath + `" -silent`
}

func addToStartup(w fyne.Window) {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	if isInStartup(exePath) {
		return
	}
	dialog.ShowConfirm(
		"Startup",
		"Launch 2cup Chat automatically when you sign in to Windows, so you don't miss offline messages?",
		func(confirmed bool) {
			if !confirmed {
				return
			}
			key, err := registry.OpenKey(registry.CURRENT_USER, startupRegKey,
				registry.SET_VALUE|registry.QUERY_VALUE)
			if err != nil {
				dialog.ShowError(fmt.Errorf("could not open registry: %w", err), w)
				return
			}
			defer key.Close()
			if err := key.SetStringValue(startupValueName, startupCommand(exePath)); err != nil {
				dialog.ShowError(fmt.Errorf("could not write to registry: %w", err), w)
			}
		},
		w,
	)
}

// ─────────────────────────────────────────────────────────────────────────────
// main
// ─────────────────────────────────────────────────────────────────────────────

// ADDED: Global variables to be accessible in onAppReady
var (
	silentStart bool
	mainWindow  fyne.Window
)

func main() {
	// ADDED: hide the empty console/cmd window that shows up on startup if
	// the exe was built without -H=windowsgui. Must run before anything
	// else prints to stdout/stderr.
	hideOwnConsoleWindow()

	silentStart = false
	for _, a := range os.Args[1:] {
		if a == "-silent" || a == "--silent" {
			silentStart = true
		}
	}

	if dir, derr := os.UserConfigDir(); derr == nil {
		logDir := filepath.Join(dir, "2cup-chat")
		_ = os.MkdirAll(logDir, 0755)
		if lf, lerr := os.OpenFile(filepath.Join(logDir, "app.log"),
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); lerr == nil {
			log.SetOutput(lf)
		}
	}
	log.Println("=== 2cup starting ===")

	rand.Seed(time.Now().UnixNano())
	a := app.NewWithID("com.2cup.p2p")
	a.Settings().SetTheme(win95Theme{})
	loadStats()

	var appIcon fyne.Resource
	if exePath, ierr := os.Executable(); ierr == nil {
		iconPath := filepath.Join(filepath.Dir(exePath), "2cup.png")
		if data, rerr := os.ReadFile(iconPath); rerr == nil {
			appIcon = fyne.NewStaticResource("2cup.png", data)
			a.SetIcon(appIcon)
		} else {
			log.Printf("icon not loaded (%s): %v", iconPath, rerr)
		}
	}

	cm, _ := connmgr.NewConnManager(50, 200, connmgr.WithGracePeriod(time.Minute))
	globalConnMgr = cm
	globalCtx, globalCancel = context.WithCancel(context.Background())
	var err error
	globalHost, err = libp2p.New(
		libp2p.ListenAddrStrings(
			"/ip4/0.0.0.0/tcp/0",
			"/ip4/0.0.0.0/udp/0/quic-v1",
			"/ip4/0.0.0.0/tcp/443",
		),
		libp2p.EnableRelay(),
		// CHANGED: the old static list pointed at IPFS DHT bootstrap peers,
		// which are not circuit-relay-v2 relays, so this was effectively a
		// no-op. relayPeerSource discovers *actual* relay-capable 2cup
		// peers dynamically (see runSelfRelayAdvertiser below) - this is
		// what lets a peer on mobile data / behind CGNAT reach a peer on a
		// different network at all.
		libp2p.EnableAutoRelayWithPeerSource(relayPeerSource),
		libp2p.ConnectionManager(cm),
		libp2p.EnableRelayService(),
		libp2p.NATPortMap(),
		libp2p.EnableHolePunching(),
	)
	if err != nil {
		log.Fatal(err)
	}

	registerDMStreamHandler() // ADDED: Register direct DM stream handler

	globalDHT, err = dht.New(globalCtx, globalHost, dht.Mode(dht.ModeAutoServer))
	if err != nil {
		log.Fatal(err)
	}
	globalDHT.Bootstrap(globalCtx)

	// ADDED: start advertising this peer as a relay if/when AutoNAT decides
	// it's publicly reachable, and keep discovering other such peers to use
	// as relays ourselves (see the "dynamic relay mesh" comment above).
	go runSelfRelayAdvertiser(globalCtx)

	globalPS, err = pubsub.NewGossipSub(globalCtx, globalHost,
		pubsub.WithFloodPublish(true),
		pubsub.WithMaxMessageSize(6*1024*1024))
	if err != nil {
		log.Fatal(err)
	}

	globalPing = ping.NewPingService(globalHost)
	_ = globalPing

	runSwarmListener()
	go bootstrap(globalCtx)

	go func() {
		tk := time.NewTicker(30 * time.Second)
		defer tk.Stop()
		for {
			select {
			case <-globalCtx.Done():
				return
			case <-tk.C:
				if globalHost != nil {
					recordOnline(len(globalHost.Network().Peers()))
				}
			}
		}
	}()

	mainWindow = a.NewWindow("2cup — P2P Chat") // ADDED: Using global mainWindow
	mainWindow.Resize(fyne.NewSize(980, 700))
	mainWindow.CenterOnScreen()
	if appIcon != nil {
		mainWindow.SetIcon(appIcon)
	}

	if desk, ok := a.(desktop.App); ok {
		mainWindow.SetCloseIntercept(func() { mainWindow.Hide() })
		trayMenu := fyne.NewMenu("2cup",
			fyne.NewMenuItem("Open 2cup", func() { mainWindow.Show() }),
			fyne.NewMenuItem("Quit", func() {
				runShutdown()
				a.Quit()
			}),
		)
		desk.SetSystemTrayMenu(trayMenu)
		if appIcon != nil {
			desk.SetSystemTrayIcon(appIcon)
		}
	}
	mainWindow.SetOnClosed(runShutdown)

	// ADDED: Check EULA acceptance before showing anything
	if eulaAccepted() {
		onAppReady()
	} else {
		showEULA(a, onAppReady)
	}

	a.Run()
}

// ADDED: onAppReady function extracted from EULA callback
func onAppReady() {
	profile := loadProfile()
	if profile != nil {
		go runPersonalListener(profile.ID)
	}

	// Silent autostart + we already have an account: stay in the tray
	if silentStart && profile != nil {
		// Do not show window, do not ask for startup. Just run in background.
		return
	}

	showMainMenu(mainWindow)
	mainWindow.Show()

	if !silentStart {
		addToStartup(mainWindow)
	}
}