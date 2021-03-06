package relay

import (
	fmt "fmt"
	"sync"

	"github.com/dedis/onet/log"
	"github.com/nikkolasg/mulsigo/event"
	net "github.com/nikkolasg/mulsigo/network"
)

// ChannelQueueSize represents how large the queue of message for one
// channel is.
const ChannelQueueSize = 100

// ChannelSize represents the maximum number of participants in a channel.
const ChannelSize = 30

// MaxChannel represents the maximum number of channels allowed for this relay.
// Let's start slowly :)
const MaxChannel = 50

// Relay is the main struct that keep tracks of all clients and channels
// active.
type Relay struct {
	router *net.Router

	channels    map[string]*channel
	channelsMut sync.Mutex
}

// NewRelay returns a relay that can be `Start()`ed with `Start`.
func NewRelay(r *net.Router) *Relay {
	relay := &Relay{
		router:   r,
		channels: make(map[string]*channel),
	}
	r.RegisterProcessor(relay, RelayMessage{})
	r.Register(net.EventConnDown, relay)
	return relay
}

// Start starts the listening process to allow incoming connections.
func (r *Relay) Start() {
	r.router.Start()
}

func (r *Relay) Process(from net.Address, msg net.Message) {
	r.channelsMut.Lock()
	defer r.channelsMut.Unlock()
	switch m := msg.(type) {
	case *RelayMessage:
		id := m.GetChannel()
		if id == "" {
			log.Warn("relay: relay message without a channel id")
			return
		}
		ch, ok := r.channels[id]
		if !ok {
			if len(r.channels) > MaxChannel {
				// too many channels
				r.router.Send(from, &RelayMessage{
					Type: RelayMessage_JOIN_RESPONSE,
					JoinResponse: &JoinResponse{
						Status: JoinResponse_FAILURE,
						Reason: "too many channels",
					},
				})
				return
			}
			// create a new one
			ch = newChannel(r, id)
			r.channels[id] = ch
		}
		ch.newMessage(from, m)
	default:
		log.Warn("received unknown msg from ", from)
	}
}

// Stop closes all channel, and closes all connections to the Relay.
func (r *Relay) Stop() {
	r.router.Stop()
	r.channelsMut.Lock()
	defer r.channelsMut.Unlock()
	for _, ch := range r.channels {
		fmt.Println("channel ", ch.id, " stop() BEFORE")
		ch.stop()
		fmt.Println("channel ", ch.id, " stop() AFTER")
	}
}

func (r *Relay) deleteChannel(id string) {
	r.channelsMut.Lock()
	defer r.channelsMut.Unlock()
	delete(r.channels, id)
}

func (r *Relay) Receive(e event.Event) {
	if e.Name() != net.EventConnDown {
		return
	}
	down := e.(*net.EventDown)
	r.channelsMut.Lock()
	defer r.channelsMut.Unlock()
	for id, ch := range r.channels {
		ch.newMessage(down.Address, &RelayMessage{Channel: id, Type: RelayMessage_LEAVE})
	}
}

// channel holds a list of all participants registered to a channel designated
// by an ID. Each participant can broadcast message to a channel and receive
// message from others on the same channel.
type channel struct {
	sync.Mutex
	id                string           // id of the channel
	relay             *Relay           // router to send messages
	incoming          chan messageInfo // incoming message coming from the router
	join              chan net.Address // join message
	leave             chan net.Address // leave message
	finished          chan bool        // stop signal from the router
	finishedConfirmed chan bool
	clients           map[net.Address]bool // list of clients. Not concurrent safe.
	done              bool                 // local variable
}

// newChannel returns a new channel identified by "id". It launches the
// processing routine.
func newChannel(r *Relay, id string) *channel {
	ch := &channel{
		id:                id,
		incoming:          make(chan messageInfo, ChannelQueueSize),
		join:              make(chan net.Address, ChannelQueueSize),
		leave:             make(chan net.Address, ChannelQueueSize),
		finished:          make(chan bool, 1),
		finishedConfirmed: make(chan bool, 1),
		relay:             r,
		clients:           make(map[net.Address]bool),
	}
	go ch.process()
	return ch
}

func (ch *channel) newMessage(from net.Address, incoming *RelayMessage) {
	switch incoming.GetType() {
	case RelayMessage_JOIN:
		ch.join <- from
	case RelayMessage_LEAVE:
		ch.leave <- from
	case RelayMessage_INGRESS:
		ch.incoming <- messageInfo{from, incoming.GetIngress()}
	default:
		log.Error("channel: unknown message type")
	}
}

func (ch *channel) process() {
	clients := ch.clients
	for {
		fmt.Printf("channel %s select on channels ....", ch.id)
		select {
		case <-ch.finished:
			fmt.Print("finished!\n")
			fmt.Println("ch.finished true !")
			//ch.finishedConfirmed <- true
			log.Lvl2("channel", ch.id, "finished")
			return
		case addr := <-ch.join:
			fmt.Print("join!\n")
			log.Lvl2("channel", ch.id, ": adding client", addr.String())
			ch.addClient(addr)
		case addr := <-ch.leave:
			fmt.Print("leave!\n")
			delete(clients, addr)
			if len(clients) == 0 {
				// delete this channel
				ch.setDone()
				ch.relay.deleteChannel(ch.id)
				return
			}
		case info := <-ch.incoming:
			fmt.Print("incoming!\n")
			ingress := info.msg
			addr := info.address
			_, ok := clients[addr]
			if !ok {
				// unknown user
				log.Lvl2("channel: msg from unregistered user", addr)
				continue
			}

			rm := &RelayMessage{
				Channel: ch.id,
				Type:    RelayMessage_EGRESS,
				Egress: &Egress{
					Blob: ingress.GetBlob(),
				},
			}

			for c := range clients {
				if c == addr {
					continue
				}
				// XXX change to a more abstract way
				if err := ch.relay.router.Send(c, rm); err != nil {
					log.Errorf("channel %d: %s: %s", ch.id, c, err)
				}
			}
		}
	}
}

// addClient adds the client to the list of local clients if capacity is not
// exceeded and replay with a JOIN_ACK message.
func (ch *channel) addClient(client net.Address) {
	jr := &JoinResponse{}

	_, ok := ch.clients[client]

	if ok {
		log.Lvl2(ch.id, "already added client", client)
		jr.Status = JoinResponse_OK
	} else if len(ch.clients) < ChannelSize {
		ch.clients[client] = true
		jr.Status = JoinResponse_OK
		log.Lvl2("adding client", client)
	} else {
		jr.Status = JoinResponse_FAILURE
		jr.Reason = "channel: can't join a full channel"
		log.Lvl2("refusing client", client)
	}

	if err := ch.relay.router.Send(client, &RelayMessage{
		Channel:      ch.id,
		Type:         RelayMessage_JOIN_RESPONSE,
		JoinResponse: jr,
	}); err != nil {
		log.Error(err)
	}
}

// stop makes the channel stop for processing messages.
func (ch *channel) stop() {
	if ch.isDone() {
		return
	}
	ch.setDone()
	close(ch.finished)
	/*fmt.Println("channel", ch.id, " wait finishedConfirmed BEFORE")*/
	//<-ch.finishedConfirmed
	/*fmt.Println("channel", ch.id, " wait finishedConfirmed AFTER")*/
}

func (ch *channel) setDone() {
	ch.Lock()
	defer ch.Unlock()
	ch.done = true
}

func (ch *channel) isDone() bool {
	ch.Lock()
	defer ch.Unlock()
	return ch.done
}

// messageInfo is a simple wrapper to wrap the sender of a message to the
// message in question.
type messageInfo struct {
	address net.Address
	msg     *Ingress
}
