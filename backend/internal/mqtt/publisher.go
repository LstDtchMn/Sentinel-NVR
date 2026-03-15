// Package mqtt provides an MQTT event bridge that publishes Sentinel NVR events
// to an MQTT broker for Home Assistant and other home automation integrations.
package mqtt

import (
	"encoding/json"
	"fmt"
	"log/slog"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/eventbus"
)

// Publisher subscribes to the event bus and forwards events to an MQTT broker.
type Publisher struct {
	client pahomqtt.Client
	prefix string
	bus    *eventbus.Bus
	logger *slog.Logger
	done   chan struct{}
	stopCh chan struct{}
}

// NewPublisher creates an MQTT publisher with the given broker connection settings.
func NewPublisher(broker, prefix, username, password string, bus *eventbus.Bus, logger *slog.Logger) *Publisher {
	opts := pahomqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID("sentinel-nvr").
		SetAutoReconnect(true).
		SetWill(prefix+"/availability", "offline", 1, true)

	if username != "" {
		opts.SetUsername(username).SetPassword(password)
	}

	return &Publisher{
		client: pahomqtt.NewClient(opts),
		prefix: prefix,
		bus:    bus,
		logger: logger.With("component", "mqtt"),
		done:   make(chan struct{}),
		stopCh: make(chan struct{}),
	}
}

// Start connects to the MQTT broker and begins forwarding events.
func (p *Publisher) Start() error {
	token := p.client.Connect()
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("mqtt connect: %w", token.Error())
	}

	// Publish online availability
	p.publish(p.prefix+"/availability", "online", true)

	// Subscribe to event bus
	go p.run()

	p.logger.Info("MQTT publisher started")
	return nil
}

func (p *Publisher) run() {
	defer close(p.done)
	ch := p.bus.Subscribe("*")
	for {
		select {
		case <-p.stopCh:
			p.bus.Unsubscribe(ch)
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			p.handleEvent(event)
		}
	}
}

func (p *Publisher) handleEvent(event eventbus.Event) {
	switch event.Type {
	case "detection":
		label := event.Label
		if label == "" {
			label = "unknown"
		}
		topic := fmt.Sprintf("%s/events/%s/%s", p.prefix, event.CameraName, label)
		payload := map[string]any{
			"camera":     event.CameraName,
			"camera_id":  event.CameraID,
			"label":      event.Label,
			"confidence": event.Confidence,
			"snapshot":   event.Thumbnail != "", // boolean, never leak filesystem path
			"timestamp":  event.Timestamp.Unix(),
		}
		p.publishJSON(topic, payload)

	case "camera.offline", "camera.online", "camera.restarted":
		topic := fmt.Sprintf("%s/cameras/%s/status", p.prefix, event.CameraName)
		p.publish(topic, event.Type, false)

	case "face_match":
		topic := fmt.Sprintf("%s/events/%s/face", p.prefix, event.CameraName)
		p.publishJSON(topic, event.Data)

	case "audio_detection":
		topic := fmt.Sprintf("%s/events/%s/audio", p.prefix, event.CameraName)
		payload := map[string]any{
			"camera":     event.CameraName,
			"camera_id":  event.CameraID,
			"label":      event.Label,
			"confidence": event.Confidence,
			"timestamp":  event.Timestamp.Unix(),
		}
		p.publishJSON(topic, payload)
	}
}

func (p *Publisher) publish(topic, payload string, retained bool) {
	token := p.client.Publish(topic, 1, retained, payload)
	if token.Wait() && token.Error() != nil {
		p.logger.Warn("mqtt publish failed", "topic", topic, "error", token.Error())
	}
}

func (p *Publisher) publishJSON(topic string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		p.logger.Warn("mqtt marshal failed", "error", err)
		return
	}
	p.publish(topic, string(data), false)
}

// Stop disconnects from the MQTT broker and stops the event forwarding loop.
func (p *Publisher) Stop() {
	select {
	case <-p.stopCh:
		// already stopped
	default:
		close(p.stopCh)
	}
	<-p.done
	p.publish(p.prefix+"/availability", "offline", true)
	p.client.Disconnect(1000)
	p.logger.Info("MQTT publisher stopped")
}
