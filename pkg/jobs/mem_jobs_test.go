package jobs

import (
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func randomMicro(min, max int) time.Duration {
	return time.Duration(rand.Intn(max-min)+min) * time.Microsecond
}

func TestInMemoryJobs(t *testing.T) {
	n := 1000
	v := 100

	var w sync.WaitGroup

	var workersTestList = WorkersList{
		"test": {
			Concurrency: 4,
			WorkerFunc: func(m *Message) error {
				var msg string
				err := m.Unmarshal(&msg)
				if !assert.NoError(t, err) {
					return err
				}
				if strings.HasPrefix(msg, "a-") {
					_, err := strconv.Atoi(msg[len("a-"):])
					assert.NoError(t, err)
				} else if strings.HasPrefix(msg, "b-") {
					_, err := strconv.Atoi(msg[len("b-"):])
					assert.NoError(t, err)
				} else {
					t.Fatal()
				}
				w.Done()
				return nil
			},
		},
	}

	w.Add(2)

	go func() {
		broker := NewMemBroker("cozy.local", workersTestList)
		for i := 0; i < n; i++ {
			w.Add(1)
			msg, _ := NewMessage(JSONEncoding, "a-"+strconv.Itoa(i+1))
			_, err := broker.PushJob(&JobRequest{
				WorkerType: "test",
				Message:    msg,
			})
			assert.NoError(t, err)
			time.Sleep(randomMicro(0, v))
		}
		w.Done()
	}()

	go func() {
		broker := NewMemBroker("cozy.local", workersTestList)
		for i := 0; i < n; i++ {
			w.Add(1)
			msg, _ := NewMessage(JSONEncoding, "b-"+strconv.Itoa(i+1))
			_, err := broker.PushJob(&JobRequest{
				WorkerType: "test",
				Message:    msg,
			})
			assert.NoError(t, err)
			time.Sleep(randomMicro(0, v))
		}
		w.Done()
	}()

	w.Wait()
}

func TestUnknownWorkerError(t *testing.T) {
	broker := NewMemBroker("baz.quz", WorkersList{})
	_, err := broker.PushJob(&JobRequest{
		WorkerType: "nope",
		Message:    nil,
	})
	assert.Error(t, err)
	assert.Equal(t, ErrUnknownWorker, err)
}

func TestUnknownMessageType(t *testing.T) {
	var w sync.WaitGroup

	broker := NewMemBroker("foo.bar", WorkersList{
		"test": {
			Concurrency: 4,
			WorkerFunc: func(m *Message) error {
				var msg string
				err := m.Unmarshal(&msg)
				assert.Error(t, err)
				assert.Equal(t, ErrUnknownMessageType, err)
				w.Done()
				return nil
			},
		},
	})

	w.Add(1)
	_, err := broker.PushJob(&JobRequest{
		WorkerType: "test",
		Message: &Message{
			Type: "unknown",
			Data: nil,
		},
	})

	assert.NoError(t, err)
	w.Wait()
}