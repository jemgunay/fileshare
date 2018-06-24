# memoryshare

### Preface
Memories documented in the form of photos and videos will eventually become saturated over time - eventually, finding a specific memory will be like finding a needle in a haystack. A further issue is that people have different collections of memories which are not always accessible to all who share the memories.

This is an WIP project which will aims to address these issues and preserve memories.

### Features

* Ability to upload files into a temporary area where a description, relevant tags and people can be added to the each file. The files can then be published so that other users can view and search them.
* Flexible search controls.
* Data querying HTTP API.
* User accounts and permissions which limit access to memories. Guest accounts can also be created which cannot upload new memories.
* Mobile responsive.

### Usage

```go
cd $GOPATH/src/github.com/jemgunay/memoryshare/cmd/memoryshare
go build && ./memoryshare
```

Following this, create a default account via the command line as instructed then connect to the server via your browser (http://localhost:8000 by default).

### Future

Once the (endless) list of core features has eventually been implemented, I intend to make the service distributed so that multiple users can run an instance of the server and be inter-connected, resulting in an consistently eventually consistent data store mesh network.
