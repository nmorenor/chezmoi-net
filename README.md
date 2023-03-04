# chezmoi-net

This is a bi-directional JsonRPC2 implementation over Web Socket. The most basic example is provided under the chatclient package. You can run the example by.

- 1 Run the server
```go run cmd/server/server.go```
- 2 Run client on host mode
```go run cmd/client/client.go```
- 3 Run client on join mode
```go run cmd/client/client.go -join```

## Register rpc call

```
// Use the client package register service functions
client.RegisterService(chatClient, chatClient.Client, chatClient.target)
```

## Send rpc call

```
// use client instance to get rpcClient
rpcClient := chatClient.Client.GetRpcClientForService(*chatClient)
sname := chatClient.Client.GetServiceName(*chatClient)

if rpcClient != nil {
    var reply string
    rpcClient.Call(sname+".OnMessage", Message{Source: *chatClient.Client.Id, Message: message}, &reply)
}
```
## Bigger example

https://github.com/nmorenor/chezmoi

