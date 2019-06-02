package main

import (
	"flag"
	"log"
	"net"
	"strconv"
	"time"
)

func main() {

	msg := flag.String("msg", "", "message content")

	flag.Parse()

	conn, err := net.Dial("tcp", "127.0.0.1:7890")

	if err != nil {
		log.Fatal(err)
	}

	t := 0
	for {

		msgStr := *msg + ":" + strconv.Itoa(t)

		log.Println(msgStr)
		// 每秒发送一次消息
		_, err := conn.Write([]byte(msgStr))

		if err != nil {
			log.Fatal(err)
		}

		time.Sleep(time.Second * time.Duration(1))
		t++
	}
}
