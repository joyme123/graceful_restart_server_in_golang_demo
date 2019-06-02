package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// server的运行状态
const (
	StateInit = iota
	StateRunning
	StateShuttingDown
	StateTerminate
)

const (
	CHILD_PROCESS string = "child_process"
	SERVER_CONN   string = "server_conn"
)

var isForked bool
var serverLock sync.Mutex

func init() {
	isForked = false
}

type Server struct {
	l         net.Listener
	conns     map[int]net.Conn
	rw        sync.RWMutex
	idLock    sync.Mutex
	idCursor  int
	isChild   bool // 是否是子进程
	status    int  // 当前服务的状态
	relaxTime int
}

func NewServer() *Server {

	serverLock.Lock()
	defer serverLock.Unlock()

	s := &Server{}
	s.conns = make(map[int]net.Conn, 0)

	s.isChild = os.Getenv(CHILD_PROCESS) != ""
	s.relaxTime = 10 // 10s的终止时间

	return s
}

func (s *Server) Start(addr string) {
	var err error

	log.Printf("pid: %v \n", os.Getpid())

	s.setState(StateInit)

	if s.isChild {
		log.Println("进入子进程")
		// 通知父进程停止
		ppid := os.Getppid()

		err := syscall.Kill(ppid, syscall.SIGTERM)

		if err != nil {
			log.Fatal(err)
		}

		// 子进程， 重新监听之前的连接
		connN, err := strconv.Atoi(os.Getenv(SERVER_CONN))
		if err != nil {
			log.Fatal(err)
		}

		for i := 0; i < connN; i++ {
			f := os.NewFile(uintptr(4+i), "")
			c, err := net.FileConn(f)
			if err != nil {
				log.Print(err)
			} else {
				id := s.add(c)
				go s.handleConn(c, id)
			}
		}
	}

	s.l, err = s.getListener(addr)
	if err != nil {
		log.Fatal(err)
	}
	defer s.l.Close()

	log.Println("listen on ", addr)

	go s.handleSignal()

	s.setState(StateRunning)

	for {
		log.Println("start accept")
		conn, err := s.l.Accept()
		if err != nil {
			log.Fatal(err)
			return
		}

		log.Println("accept new conn")

		id := s.add(conn)
		go s.handleConn(conn, id)
	}

}

func (s *Server) getListener(addr string) (l net.Listener, err error) {
	if s.isChild {
		f := os.NewFile(3, "")
		l, err = net.FileListener(f)
		return
	}

	l, err = net.Listen("tcp", addr)

	return
}

func (s *Server) handleConn(conn net.Conn, id int) {
	defer conn.Close()
	buf := make([]byte, 512)

	for {
		_, err := conn.Read(buf)
		if err != nil {
			// 连接断开
			s.del(id)
			log.Println(err)
			return
		}

		log.Printf("pid %v: %s\n", os.Getpid(), string(buf))
	}
}

// 标记conn的id
func (s *Server) getID() int {
	s.idLock.Lock()
	defer s.idLock.Unlock()

	s.idCursor++
	return s.idCursor
}

func (s *Server) get(id int) net.Conn {
	s.rw.RLock()
	defer s.rw.RUnlock()

	return s.conns[id]
}

func (s *Server) add(conn net.Conn) int {

	s.rw.Lock()
	defer s.rw.Unlock()

	id := s.getID()
	s.conns[id] = conn

	return id
}

func (s *Server) del(id int) {
	s.rw.Lock()
	defer s.rw.Unlock()
	delete(s.conns, id)
}

func (s *Server) handleSignal() {
	sc := make(chan os.Signal)

	signal.Notify(sc, syscall.SIGHUP, syscall.SIGTERM)

	for {
		sig := <-sc

		switch sig {
		case syscall.SIGHUP:
			log.Println("signal sighup")
			// reload
			go func() {
				s.fork()
			}()
		case syscall.SIGTERM:
			log.Println("signal sigterm")
			// stop
			s.shutdown()
		}
	}
}

func (s *Server) setState(status int) {
	s.status = status
}

// 通过shell调用创建子进程
func (s *Server) fork() (err error) {

	log.Println("start forking")
	serverLock.Lock()
	defer serverLock.Unlock()

	if isForked {
		return errors.New("Another process already forked. Ignoring this one")
	}

	isForked = true

	files := make([]*os.File, 1+len(s.conns))
	files[0], err = s.l.(*net.TCPListener).File() // 将监听带入到子进程中
	if err != nil {
		log.Println(err)
		return
	}

	i := 1
	for _, conn := range s.conns {
		files[i], err = conn.(*net.TCPConn).File()

		if err != nil {
			log.Println(err)
			return
		}

		i++
	}

	env := append(os.Environ(), CHILD_PROCESS+"=1")
	env = append(env, fmt.Sprintf("%s=%s", SERVER_CONN, strconv.Itoa(len(s.conns))))

	path := os.Args[0] // 当前可执行程序的路径
	var args []string
	if len(os.Args) > 1 {
		args = os.Args[1:]
	}

	cmd := exec.Command(path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = files
	cmd.Env = env

	err = cmd.Start()
	if err != nil {
		log.Println(err)
		return
	}

	return
}

// shutdown 方法用来优雅的关闭服务
// 1. 关闭accept, 不接受新的连接进来
// 2. 启动等待机制， 等待当前正在处理的请求结束
func (s *Server) shutdown() {

	log.Println("shutting down server")

	// 将当前server的状态设置为shuttingdown
	if s.status != StateRunning {
		log.Fatal("server is not running")
	}

	s.setState(StateShuttingDown)

	time.Sleep(time.Second * time.Duration(s.relaxTime))

	log.Println("SHUTDOWN OK")
}

func main() {
	server := NewServer()

	server.Start("127.0.0.1:7890")
}
