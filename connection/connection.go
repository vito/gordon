package connection

import (
	"bufio"
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"sync"

	"code.google.com/p/gogoprotobuf/proto"
	warden "github.com/cloudfoundry-incubator/garden/protocol"
)

var DisconnectedError = errors.New("disconnected")

type Connection struct {
	Disconnected chan bool

	messages chan *warden.Message

	conn      net.Conn
	read      *bufio.Reader
	writeLock sync.Mutex
	readLock  sync.Mutex
}

type WardenError struct {
	Message   string
	Data      string
	Backtrace []string
}

func (e *WardenError) Error() string {
	return e.Message
}

func Connect(network, addr string) (*Connection, error) {
	conn, err := net.Dial(network, addr)
	if err != nil {
		return nil, err
	}

	return New(conn), nil
}

func New(conn net.Conn) *Connection {
	messages := make(chan *warden.Message)

	connection := &Connection{
		// buffered so that read and write errors
		// can both send without blocking
		Disconnected: make(chan bool, 2),

		messages: messages,

		conn: conn,
		read: bufio.NewReader(conn),
	}

	go connection.readMessages()

	return connection
}

func (c *Connection) Close() {
	c.conn.Close()
}

func (c *Connection) Create(properties map[string]string) (*warden.CreateResponse, error) {
	props := []*warden.Property{}
	for key, val := range properties {
		props = append(props, &warden.Property{
			Key:   proto.String(key),
			Value: proto.String(val),
		})
	}

	req := &warden.CreateRequest{Properties: props}

	res, err := c.RoundTrip(req, &warden.CreateResponse{})
	if err != nil {
		return nil, err
	}

	return res.(*warden.CreateResponse), nil
}

func (c *Connection) Stop(handle string, background, kill bool) (*warden.StopResponse, error) {
	res, err := c.RoundTrip(
		&warden.StopRequest{
			Handle:     proto.String(handle),
			Background: proto.Bool(background),
			Kill:       proto.Bool(kill),
		},
		&warden.StopResponse{},
	)

	if err != nil {
		return nil, err
	}

	return res.(*warden.StopResponse), nil
}

func (c *Connection) Destroy(handle string) (*warden.DestroyResponse, error) {
	res, err := c.RoundTrip(
		&warden.DestroyRequest{Handle: proto.String(handle)},
		&warden.DestroyResponse{},
	)

	if err != nil {
		return nil, err
	}

	return res.(*warden.DestroyResponse), nil
}

func (c *Connection) Run(handle, script string, resourceLimits *warden.ResourceLimits, environmentVariables []*warden.EnvironmentVariable) (uint32, chan *warden.ProcessPayload, error) {
	err := c.SendMessage(
		&warden.RunRequest{
			Handle:  proto.String(handle),
			Script:  proto.String(script),
			Rlimits: resourceLimits,
			Env:     environmentVariables,
		},
	)

	if err != nil {
		return 0, nil, err
	}

	responses := make(chan *warden.ProcessPayload)

	resMsg, err := c.ReadResponse(&warden.ProcessPayload{})
	if err != nil {
		return 0, nil, err
	}

	firstResponse := resMsg.(*warden.ProcessPayload)

	go func() {
		for {
			resMsg, err := c.ReadResponse(&warden.ProcessPayload{})
			if err != nil {
				close(responses)
				break
			}

			response := resMsg.(*warden.ProcessPayload)

			responses <- response

			if response.ExitStatus != nil {
				close(responses)
				break
			}
		}
	}()

	return firstResponse.GetProcessId(), responses, nil
}

func (c *Connection) Attach(handle string, processID uint32) (chan *warden.ProcessPayload, error) {
	err := c.SendMessage(
		&warden.AttachRequest{
			Handle:    proto.String(handle),
			ProcessId: proto.Uint32(processID),
		},
	)

	if err != nil {
		return nil, err
	}

	responses := make(chan *warden.ProcessPayload)

	go func() {
		for {
			resMsg, err := c.ReadResponse(&warden.ProcessPayload{})
			if err != nil {
				close(responses)
				break
			}

			response := resMsg.(*warden.ProcessPayload)

			responses <- response

			if response.ExitStatus != nil {
				close(responses)
				break
			}
		}
	}()

	return responses, nil
}

func (c *Connection) NetIn(handle string) (*warden.NetInResponse, error) {
	res, err := c.RoundTrip(
		&warden.NetInRequest{Handle: proto.String(handle)},
		&warden.NetInResponse{},
	)

	if err != nil {
		return nil, err
	}

	return res.(*warden.NetInResponse), nil
}

func (c *Connection) LimitMemory(handle string, limit uint64) (*warden.LimitMemoryResponse, error) {
	res, err := c.RoundTrip(
		&warden.LimitMemoryRequest{
			Handle:       proto.String(handle),
			LimitInBytes: proto.Uint64(limit),
		},
		&warden.LimitMemoryResponse{},
	)

	if err != nil {
		return nil, err
	}

	return res.(*warden.LimitMemoryResponse), nil
}

func (c *Connection) GetMemoryLimit(handle string) (uint64, error) {
	res, err := c.RoundTrip(
		&warden.LimitMemoryRequest{
			Handle: proto.String(handle),
		},
		&warden.LimitMemoryResponse{},
	)

	if err != nil {
		return 0, err
	}

	limit := res.(*warden.LimitMemoryResponse).GetLimitInBytes()
	if limit == math.MaxInt64 { // PROBABLY NOT A LIMIT
		return 0, nil
	}

	return limit, nil
}

func (c *Connection) LimitCPU(request *warden.LimitCpuRequest) (*warden.LimitCpuResponse, error) {
	res, err := c.RoundTrip(
		request,
		&warden.LimitCpuResponse{},
	)

	if err != nil {
		return nil, err
	}

	return res.(*warden.LimitCpuResponse), nil
}

func (c *Connection) LimitDisk(request *warden.LimitDiskRequest) (*warden.LimitDiskResponse, error) {
	res, err := c.RoundTrip(
		request,
		&warden.LimitDiskResponse{},
	)

	if err != nil {
		return nil, err
	}

	return res.(*warden.LimitDiskResponse), nil
}

func (c *Connection) GetDiskLimit(handle string) (uint64, error) {
	res, err := c.RoundTrip(
		&warden.LimitDiskRequest{
			Handle: proto.String(handle),
		},
		&warden.LimitDiskResponse{},
	)

	if err != nil {
		return 0, err
	}

	return res.(*warden.LimitDiskResponse).GetByteLimit(), nil
}

func (c *Connection) CopyIn(handle, src, dst string) (*warden.CopyInResponse, error) {
	res, err := c.RoundTrip(
		&warden.CopyInRequest{
			Handle:  proto.String(handle),
			SrcPath: proto.String(src),
			DstPath: proto.String(dst),
		},
		&warden.CopyInResponse{},
	)

	if err != nil {
		return nil, err
	}

	return res.(*warden.CopyInResponse), nil
}

func (c *Connection) CopyOut(handle, src, dst, owner string) (*warden.CopyOutResponse, error) {
	res, err := c.RoundTrip(
		&warden.CopyOutRequest{
			Handle:  proto.String(handle),
			SrcPath: proto.String(src),
			DstPath: proto.String(dst),
			Owner:   proto.String(owner),
		},
		&warden.CopyOutResponse{},
	)

	if err != nil {
		return nil, err
	}

	return res.(*warden.CopyOutResponse), nil
}

func (c *Connection) List(filterProperties map[string]string) (*warden.ListResponse, error) {
	props := []*warden.Property{}
	for key, val := range filterProperties {
		props = append(props, &warden.Property{
			Key:   proto.String(key),
			Value: proto.String(val),
		})
	}

	req := &warden.ListRequest{Properties: props}

	res, err := c.RoundTrip(req, &warden.ListResponse{})
	if err != nil {
		return nil, err
	}

	return res.(*warden.ListResponse), nil
}

func (c *Connection) Info(handle string) (*warden.InfoResponse, error) {
	res, err := c.RoundTrip(&warden.InfoRequest{
		Handle: proto.String(handle),
	}, &warden.InfoResponse{})
	if err != nil {
		return nil, err
	}

	return res.(*warden.InfoResponse), nil
}

func (c *Connection) RoundTrip(request proto.Message, response proto.Message) (proto.Message, error) {
	err := c.SendMessage(request)
	if err != nil {
		return nil, err
	}

	resp, err := c.ReadResponse(response)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *Connection) SendMessage(req proto.Message) error {
	c.writeLock.Lock()
	defer c.writeLock.Unlock()

	request, err := proto.Marshal(req)
	if err != nil {
		return err
	}

	msg := &warden.Message{
		Type:    warden.TypeForMessage(req).Enum(),
		Payload: request,
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	_, err = c.conn.Write(
		[]byte(
			fmt.Sprintf(
				"%d\r\n%s\r\n",
				len(data),
				data,
			),
		),
	)

	if err != nil {
		c.disconnected()
		return err
	}

	return nil
}

func (c *Connection) readMessages() {
	for {
		payload, err := c.readPayload()
		if err != nil {
			c.disconnected()
			close(c.messages)
			break
		}

		message := &warden.Message{}
		err = proto.Unmarshal(payload, message)
		if err != nil {
			continue
		}

		c.messages <- message
	}
}

func (c *Connection) disconnected() {
	c.Disconnected <- true
}

func (c *Connection) ReadResponse(response proto.Message) (proto.Message, error) {
	message, ok := <-c.messages
	if !ok {
		return nil, DisconnectedError
	}

	if message.GetType() == warden.Message_Error {
		errorResponse := &warden.ErrorResponse{}
		err := proto.Unmarshal(message.Payload, errorResponse)
		if err != nil {
			return nil, errors.New("error unmarshalling error!")
		}

		return nil, &WardenError{
			Message:   errorResponse.GetMessage(),
			Data:      errorResponse.GetData(),
			Backtrace: errorResponse.GetBacktrace(),
		}
	}

	responseType := warden.TypeForMessage(response)
	if message.GetType() != responseType {
		return nil, errors.New(
			fmt.Sprintf(
				"expected message type %s, got %s\n",
				responseType.String(),
				message.GetType().String(),
			),
		)
	}

	err := proto.Unmarshal(message.GetPayload(), response)

	return response, err
}

func (c *Connection) readPayload() ([]byte, error) {
	c.readLock.Lock()
	defer c.readLock.Unlock()

	msgHeader, err := c.read.ReadBytes('\n')
	if err != nil {
		return nil, err
	}

	msgLen, err := strconv.ParseUint(string(msgHeader[0:len(msgHeader)-2]), 10, 0)
	if err != nil {
		return nil, err
	}

	payload, err := readNBytes(int(msgLen), c.read)
	if err != nil {
		return nil, err
	}

	_, err = readNBytes(2, c.read) // CRLN
	if err != nil {
		return nil, err
	}

	return payload, err
}

func readNBytes(payloadLen int, io *bufio.Reader) ([]byte, error) {
	payload := make([]byte, payloadLen)

	for readCount := 0; readCount < payloadLen; {
		n, err := io.Read(payload[readCount:])
		if err != nil {
			return nil, err
		}

		readCount += n
	}

	return payload, nil
}
