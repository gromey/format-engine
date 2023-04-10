# format-engine

![https://img.shields.io/github/v/tag/gromey/format-engine](https://img.shields.io/github/v/tag/gromey/format-engine)
![https://img.shields.io/github/license/gromey/format-engine](https://img.shields.io/github/license/gromey/format-engine)

`format-engine` is a library designed to create custom formatters like `JSON`, `XML`.

## Installation

`format-engine` can be installed like any other Go library through `go get`:

```console
$ go get github.com/gromey/format-engine
```

Or, if you are already using
[Go Modules](https://github.com/golang/go/wiki/Modules), you may specify a version number as well:

```console
$ go get github.com/gromey/format-engine@latest
```

## Getting Started

After you get the library, you must generate your type using the following command:

```console
$ go run github.com/gromey/format-engine/cmd/create-format -n=name
```

In this command, you must specify a name of your new formatter. The name must contain only letters and be as simple as possible.

This command generates a package with the name specified in the generate command.   
The package will contain two files `asserts.go` and `tag.go`.

**WARNING:** DO NOT EDIT `asserts.go`.

`tag.go` will contain the base implementation of your new formatter. You need to implement two functions **Encode** and **Decode**.

**Encode** function receives a value encoded into a byte array, if exists a tag value and a field name,    
here you can do additional encoding or return the byte array unchanged.

**Decode** function receives an encoded data, if exists a tag value and a field name, here you must find a byte array   
representing a value for the current field and perform initial decoding if necessary before returning this byte array.  
You can change the input data and for the next field you will receive the data in a modified form,    
however this will not affect the original data, since you are working with a copy of the data.
