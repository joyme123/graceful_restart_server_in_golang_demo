package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

type Server struct {
	l        net.Listener
	conns    map[int]net.Conn
	rw       sync.RWMutex
	idLock   sync.Mutex
	idCursor int
}

func NewServer() *Server {
	s := &Server{}
	s.conns = make(map[int]net.Conn, 0)
	return s
}

func (s *Server) Start() {
	var err error
	s.l, err = net.Listen("tcp", "0.0.0.0:12345")
	if err != nil {
		log.Println(err)
	}
	defer s.l.Close()

	s.handleSignal()

	for {
		conn, err := s.l.Accept()
		if err != nil {
			log.Fatal(err)
			return
		}

		id := s.add(conn)
		s.handleConn(conn, id)
	}

}

func (s *Server) handleConn(conn net.Conn, id int) {
	defer conn.Close()
	buf := make([]byte, 512)

	for {
		n, err := conn.Read(buf)
		if err != nil {
			// 连接断开
			s.del(id)
			log.Println(err)
			return
		}

		log.Printf("读取了 %d 个字节, 内容为: %s\n", n, string(buf))
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
			// reload
			go func() {
				s.fork()
			}()
		case syscall.SIGTERM:
			// stop
			s.shutdown()
		}
	}
}

// 通过shell调用创建子进程
func (s *Server) fork() {

}

func (s *Server) shutdown() {

}
