package main

import (
	"flag"
	"fmt"
	"github.com/os-mk/ratp"
	"io"
	"log"
	"os"
)

var port = flag.String("port", "", "serial port to use")
var cfg = flag.String("cfg", "115200,8E1", "configuration for serial port")
var verbose = flag.Bool("verbose", false, "verbose mode")
var mdl = flag.Int("mdl", 255, "MDL (1 ≤ MDL ≤ 255)")

func main() {
	flag.Parse()

	if *mdl < 1 || *mdl > 255 {
		fmt.Fprintln(os.Stderr, "Incorrect MDL value")
		os.Exit(1)
	}

	if len(*port) == 0 {
		fmt.Fprintln(os.Stderr, "Port must be chosen")
		os.Exit(2)
	}

	var r *ratp.RATP
	if *verbose {
		r = ratp.New(log.New(os.Stderr, "", log.LstdFlags))
	} else {
		r = ratp.New(nil)
	}

	conn, err := r.Connect(*port, *cfg, *mdl)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
	defer conn.Close()

	n, err := io.Copy(conn, os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(4)
	}

	if *verbose {
		fmt.Fprintf(os.Stderr, "%d byte sent\n", n)
	}

	n, err = io.Copy(os.Stdout, conn)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(4)
	}

	if *verbose {
		fmt.Fprintf(os.Stderr, "%d byte read\n", n)
	}
}
