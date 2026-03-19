# go-template-cli

This is a quick and simple example of a Go CLI application. The [Cobra](https://github.com/spf13/cobra) library is used to handle argument and flag parsing.

### Built With

* [![Golang][golang-shield]][golang-url]

## Usage

Execute `go-template-cli help` for more detailed information.

Command    | Arguments              | Description
---------- | ---------------------- | -----------
subcommand | `[flags] <input-text>` | Example argument handling

## Development

### Build

```go
> go build
```

### Debugging

Using the [Delve][delve-url] debugger with CLI applications is a little tricky. See the [Delve documentation][delve-debug-url] for recommended procedures on how to do this.

### Release

While the produced binary is a CLI application and is intended to be executed by directly, a containerized installation is also provided. This container utilizes a dedicated build stage along with the [scratch][scratch-url] Docker image to ensure the final image contains only the necessary resources and nothing else.

#### Build
```sh
> docker build . -t go-template-cli
> docker run go-template-cli subcommand "Hello, World!"
```

## License

This example code is provided to the public domain via the CC0 1.0 Universal License. See [LICENSE.md](./LICENSE.md) for more information.


<!-- Reference Links -->
[golang-url]: https://go.dev
[golang-shield]: https://img.shields.io/badge/golang-09657c?style=for-the-badge&logo=go&logoColor=79d2fa
[delve-url]: https://github.com/go-delve/delve
[delve-debug-url]: https://github.com/go-delve/delve/blob/master/Documentation/faq.md#-how-can-i-use-delve-to-debug-a-cli-application
[scratch-url]: https://hub.docker.com/_/scratch/