### fileshare

[ under development/unreleased ]

Memories documented in the form of photos and videos will eventually become saturated over time, making finding a specific memory like finding a needle in a haystack. A further issue is that people have different collections of memories which are not always accessible to all who share the memories.

I intend to develop a distributed server which can be deployed by any number of people who wish to join the memory sharing network. This will ensure that everyone shares the same set of files and that the files are assigned metadata which can be used to identify and locate them. For those who choose not to host a file server, a HTTP interface will provide access from file hosters who permit it.   
* Decentralised: multiple servers can have a copy of all files which will be accessible when all other hosts are offline.
* Flexible: anyone can drop offline and will collect file updates on next start up (assuming other file hosting servers are online).
* Scalable: new hosts can join at any time and will begin collecting unacquired files from other online hosts. 
* Authenticated: only permitted users can access and contribute files.