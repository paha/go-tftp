# In-memory TFTP Server

This is a simple in-memory TFTP server, implemented in Go.  It is
[RFC1350][1]-compliant, but doesn't implement the additions in later RFCs.  In
particular, options are not recognized.

## Usage

### Build and run

The project has no external dependencies. Standard libraries and provided `wire` package (curtesy of [igneous.io][2]) are used.

```bash
$ go build
$ ./go-tftp
```

### Additional notes

* The in-memory store has no limitation on size, mind memory usage when testing and using to prevent OOM.
* Only _binary_ mode is supported.
* Note, unit tests are missing, pending another evening of fun.
* The general purpose logger outputs to STDOUT, suitable to be packaged as a container.
* There is another logger,  a _registry_ of all RRQs and WRQs written to a file.

```bash
      -Max-timeout int
            TFTP max timeout (default 25)
      -Rexmt-interval int
            TFTP Retransmit interval (default 5)
      -host string
            TFTP interface address (default "127.0.0.1:6969")
      -registryFile string
            TFTP WRQ/RRQ registry. (default "tftpRegistry.log")
```

## Testing

* Any `tftp` client can be used for testing. make sure to use _binary_ mode.
* Use `-race` if using `go run` e.g. `go run -race *.go`.

Example bash function to send a file:

```bash
function tftp_send() {
  tftp $HOST $PORT << TFTP
  verb
  trace
  binary
  put $FILE
TFTP
}
```

Fetch a file:

```bash
function tftp_get() {
  tftp $HOST $PORT << TFTP
  verb
  trace
  binary
  get $FILE back-$FILE
TFTP
}
```

## _Coming soon_

* Unit tests.
* CI/CD, to validate tests, and package.
* Awesome GitHub badges :)
* Refactoring.

Feel free to reach out with comments etc.

[1]: https://tools.ietf.org/html/rfc1350
[2]; https://www.igneous.io/
