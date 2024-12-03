# Introduction

VBeam is a minimal web app engine.

Its core distinguishing feature is allowing you to define server side procedures
that can be called from the client side (RPC).

It exposes the ServeMux so you can customize it and add your own routes if you
so wish.

# Procedure Basics

A procedure that can be registered as an RPC must follow a signature like this:

```go
func PublicName(ctx *vbeam.Context, request InputParams) (response OutputData, err error) {

}
```

The `InputParams` and `OutputData` change depending on the procedure.

The context and error parameters are mandetory.

You may notice that nothing about the procedure's definition has anything to do
with HTTP.

Yes, there's a mysterious 'ctx' parameter, but it does not in fact have any info
about the http request, as we will see below.

This means that this procedure can also be called in different ways. For example,
you can create a small "script"; a stand along program that imports your package
and calls its public procedures.

## The Context

The context serves two purposes:

- Extract information from the HTTP request that mediated this procedure call
- Maintain the database transaction and switch between read/write modes.

This frees the procedure from the burden of having to release the transaction,
and of having to parse incoming http request or serialize the response data or
error.

There are only three properties on the context:

- The Application Name
- The Session Token
- The vbolt Transaction

The meaning of the session token is application specific, but the expectation is
that it maps internally to a user session and that you can use it to extract
the logged in user id.

If you call a procedure like this from a "script", you need to have a valid
session token. Perhaps you can first generate a session and then add its session
token to the context before you call the procedure.

## The database

VBeam assumes you want to use [VBolt](https://pkg.go.dev/go.hasen.dev/vbolt): a
simple and robust data storage and retrival system. Refer to its documentation.

In essense, it gives you a programmatic interface for storing and retriving data.
No textual query lanuage, no impedence mismatch between your domain models and
persistence models.

## "ReST"

VBeam does not respect the notion of "RESTful APIs". There are no "resources",
just procedures. All remote procedure calls use the http POST method. We don't
care about the ReST jargon.

# Application

You start by creating an instance of `vbeam.Application`, and registering the
procedures on it in this way:

```go
    app := MakeApplication(...) // returns *vbeam.Application
    vbeam.RegisterProc(app, MyProc1)
    vbeam.RegisterProc(app, MyProc2)
    vbeam.RegisterProc(app, MyProc3)
    vbeam.RegisterProc(app, MyProc4)
```

The `app` fulfill the role of an HTTP server in the Go program. It also contains
a reference to the VBolt database to be used for all procedure calls.

In addition to that, it's configured out of the box to serve the frontend files
and static files.

We make a distinction because frontend files are the result of bundling the
frontend's javascript files, and you would always have them available in local
dev mode.

Static files are different. They are things that you don't commit to your git
repository, instead, they are a part of the deployment environment, and might
contain things like user uploaded images.

# Generating typescript bindings

In development mode, you can add a line like this to your main function, after
you register all the procs:

```go
    vbeam.GenerateTSBindings(app, "frontend/src/server.ts")
```

This will generate a Typescript file at the given path, and it will contain type
definitions for all input and output parameters for all procedures, as well as
helper procedures that have the exact same name as their server side counter
parts.

When you call the function from client side, you provde the input params, and
you get back the output and the error message (string).

```typescript
    let [response, error] = await server.PublicName({....})
```

If the function returned an error, the response will be null.

# Local development mode

VBeam comes with a set of helper functions for running the server on your local
machine with minimal hassle.

- Frontend building and typechecking embedded in the server itself.
- TUI shows server log feed and typechecking status

# Production mode

TODO

# Working with core_server

TODO

# Frontend library

TODO
