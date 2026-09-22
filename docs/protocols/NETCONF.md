# NETCONF.md

**Project:** TriadSim — Lightweight Telecom Equipment Simulator  
**Protocol:** Network Configuration Protocol (NETCONF)  
**Transport:** SSH (RFC 6242)  
**Default Port:** 830/TCP  
**Protocol Version:** 1.1 (RFC 6241)  
**Base Capability:** `urn:ietf:params:netconf:base:1.1`

---

## 1. Introduction

The Network Configuration Protocol (NETCONF) is an Internet Standards Track protocol defined in RFC 6241. It provides mechanisms to install, manipulate, and delete the configuration of network devices. It uses an Extensible Markup Language (XML)-based data encoding for the configuration data as well as the protocol messages. NETCONF protocol operations are realized as remote procedure calls (RPCs) .

TriadSim implements NETCONF as one of its three northbound management interfaces, alongside SNMPv2c and RESTCONF. The NETCONF interface provides full CRUD (Create, Read, Update, Delete) operations on the simulated device's configuration datastores.

The NETCONF implementation in TriadSim supports:

- **Base protocol 1.1** with chunked framing (RFC 6242)
- **Candidate datastore** with `commit`, `discard-changes`, and `confirmed-commit`
- **Event notifications** via `create-subscription` (RFC 5277)
- **Writable running** for direct configuration
- **Rollback on error** and **validate** capabilities

---

## 2. Protocol Overview

### 2.1. Architecture

NETCONF uses a client-server model over a secure transport:

| Role | Entity | Description |
|---|---|---|
| **Client** | NMS, CLI, netopeer2-cli, custom scripts | Initiates NETCONF sessions, sends RPCs |
| **Server** | TriadSim simulator | Processes RPCs, manages datastores, sends notifications |

The protocol is session-based. Each session begins with a capabilities exchange (`` exchange), after which the client and server can exchange `` and `` elements.

### 2.2. Message Structure

NETCONF messages are XML documents. Every message uses the base namespace:

```xml
<rpc message-id="101" 
     xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <!-- operation -->
</rpc>
```

A corresponding reply:

```xml
<rpc-reply message-id="101"
           xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <ok/>
</rpc-reply>
```

The `` attribute is a string chosen by the sender and echoed by the receiver. All NETCONF XML elements are defined in the `urn:ietf:params:xml:ns:netconf:base:1.0` namespace, with protocol operation elements in the `urn:ietf:params:xml:ns:netconf:base:1.0` namespace as well.

### 2.3. Transport: NETCONF over SSH

NETCONF is transported over SSH as an SSH subsystem, as defined in RFC 6242. The SSH server must allow access to the `netconf` subsystem on the IANA-assigned TCP port 830 .

TriadSim uses `golang.org/x/crypto/ssh` to implement the SSH server. The subsystem name is `netconf`. No authentication is enforced in the simulator — any username/password combination is accepted.

---

## 3. Framing Protocol

### 3.1. Overview

RFC 6242 defines two framing mechanisms for NETCONF over SSH:

| Framing | Capability | Delimiter |
|---|---|---|
| **End-of-message** | `:base:1.0` | `]]>]]>` |
| **Chunked framing** | `:base:1.1` | Length-prefixed chunks |

If both peers advertise `:base:1.1`, the chunked framing mechanism defined in Section 4.2 of RFC 6242 is used for the remainder of the NETCONF session . Otherwise, the end-of-message delimiter is used.

### 3.2. Chunked Framing

Chunked framing encodes each NETCONF message as a sequence of chunks. Each chunk has a length followed by the chunk data. The message ends with a zero-length chunk:

```
#<chunk-size>\n
<chunk-data>
##\n
```

For example, a message consisting of `hello` would be encoded as:

```
#10
hello
##
```

The chunked framing mechanism ensures that character sequences within XML elements are not misinterpreted as message boundaries .

### 3.3. Capability Negotiation

The client and server exchange `` elements immediately after the SSH connection is established. The `` element contains:

- `` element with base capability
- `` element with session ID
- `` elements for each supported capability

Example `` from TriadSim:

```xml
<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <capabilities>
    <capability>urn:ietf:params:netconf:base:1.1</capability>
    <capability>urn:ietf:params:netconf:capability:candidate:1.0</capability>
    <capability>urn:ietf:params:netconf:capability:confirmed-commit:1.1</capability>
    <capability>urn:ietf:params:netconf:capability:rollback-on-error:1.0</capability>
    <capability>urn:ietf:params:netconf:capability:validate:1.1</capability>
    <capability>urn:ietf:params:netconf:capability:notification:1.0</capability>
    <capability>urn:ietf:params:netconf:capability:writable-running:1.0</capability>
  </capabilities>
  <session-id>42</session-id>
</hello>
```

---

## 4. Capabilities

TriadSim advertises the following NETCONF capabilities:

| Capability | URN | Description |
|---|---|---|
| **Base 1.1** | `urn:ietf:params:netconf:base:1.1` | Protocol version 1.1 with chunked framing |
| **Candidate** | `urn:ietf:params:netconf:capability:candidate:1.0` | Supports candidate datastore |
| **Confirmed Commit 1.1** | `urn:ietf:params:netconf:capability:confirmed-commit:1.1` | Supports confirmed commit with cancel-commit and persist |
| **Rollback on Error** | `urn:ietf:params:netconf:capability:rollback-on-error:1.0` | Supports automatic rollback on error |
| **Validate 1.1** | `urn:ietf:params:netconf:capability:validate:1.1` | Supports configuration validation |
| **Notification** | `urn:ietf:params:netconf:capability:notification:1.0` | Supports event notifications |
| **Writable Running** | `urn:ietf:params:netconf:capability:writable-running:1.0` | Supports direct edits to running datastore |

The `:candidate` capability indicates that the device supports a candidate configuration datastore, which is used to hold configuration data that can be manipulated without impacting the device's current configuration .

---

## 5. Datastores

NETCONF defines several configuration datastores. TriadSim implements the following:

| Datastore | Description | Persistence |
|---|---|---|
| **running** | Active configuration currently in use | In-memory |
| **candidate** | Work-in-progress copy of running | In-memory |
| **startup** | Configuration loaded at boot | `startup.json` on disk |

### 5.1. Running

The `running` datastore holds the complete configuration currently active on the device. Changes to `running` take effect immediately. In TriadSim, the running datastore is stored as a `map[string]any` keyed by model path.

### 5.2. Candidate

The `candidate` datastore is a temporary workspace where configuration changes can be made without affecting the running configuration. Changes made to `candidate` are committed to `running` using the `` operation .

The workflow is:

1. `` with `` target
2. `` to validate and apply changes
3. `` to discard uncommitted changes

### 5.3. Startup

The `startup` datastore holds the configuration that was loaded when the device booted. When the device restarts, `startup` is loaded into `running`. TriadSim persists `startup` as `startup.json` on disk using `encoding/json`.

---

## 6. Protocol Operations

TriadSim supports the following NETCONF operations, as defined in RFC 6241:

### 6.1. get-config

Retrieves all or part of a specified configuration datastore.

```xml
<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <get-config>
    <source><running/></source>
    <filter type="subtree">
      <radio-link xmlns="urn:sim:radio-link">
        <name>radio0</name>
      </radio-link>
    </filter>
  </get-config>
</rpc>
```

Positive response contains `` with the requested data. Negative response contains `` with an error-tag.

### 6.2. edit-config

Loads all or part of a configuration into a target datastore. The `` operation supports multiple operation types:

| Operation | Description |
|---|---|
| **merge** | Merge configuration with target (default) |
| **replace** | Completely replace target configuration |
| **create** | Create configuration, error if exists |
| **delete** | Delete configuration, error if missing |
| **remove** | Delete configuration, no error if missing |

The `merge` behavior: the configuration data in the `` parameter is merged with the configuration at the corresponding level in the target datastore. This is the default behavior .

Example:

```xml
<rpc message-id="102" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <edit-config>
    <target><candidate/></target>
    <config>
      <radio-link xmlns="urn:sim:radio-link">
        <name>radio0</name>
        <tx-power>20.0</tx-power>
        <atpc>
          <enabled>true</enabled>
          <target-rssi>-45.0</target-rssi>
        </atpc>
        <acm>
          <enabled>true</enabled>
        </acm>
      </radio-link>
    </config>
  </edit-config>
</rpc>
```

The `` element also supports `` and ``:

- **error-option**: `stop-on-error` (default), `continue-on-error`, `rollback-on-error`
- **test-option**: `test-then-set` (default), `set`, `test-only`

### 6.3. copy-config

Creates or replaces an entire configuration datastore with the contents of another.

```xml
<rpc message-id="103" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <copy-config>
    <target><startup/></target>
    <source><running/></source>
  </copy-config>
</rpc>
```

### 6.4. delete-config

Deletes a configuration datastore. The `running` datastore cannot be deleted.

```xml
<rpc message-id="104" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <delete-config>
    <target><startup/></target>
  </delete-config>
</rpc>
```

### 6.5. lock / unlock

The `` operation allows a client to lock a datastore so that only the session holding the lock can modify it. The `` operation releases the lock.

```xml
<rpc message-id="105" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <lock>
    <target><candidate/></target>
  </lock>
</rpc>
```

Locks are important for shared configurations: the configuration locking feature should be used to prevent inadvertent alteration of changes made by other sessions .

### 6.6. commit

The `` operation commits the candidate configuration to the running configuration.

**Basic commit:**

```xml
<rpc message-id="106" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <commit/>
</rpc>
```

**Confirmed commit:**

A confirmed `` operation must be reverted if a confirming commit is not issued within the timeout period (by default 600 seconds = 10 minutes) .

```xml
<rpc message-id="107" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <commit>
    <confirmed/>
    <confirm-timeout>30</confirm-timeout>
  </commit>
</rpc>
```

The confirming commit is a `` operation without the `` parameter. If the session issuing the confirmed commit is terminated before the confirm timeout expires, the server must restore the configuration to its state before the confirmed commit was issued .

**Cancel commit:**

To cancel a confirmed commit and revert changes without waiting for the timeout, the client uses the `` operation :

```xml
<rpc message-id="108" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <cancel-commit/>
</rpc>
```

### 6.7. discard-changes

Discards all uncommitted changes in the candidate datastore.

```xml
<rpc message-id="109" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <discard-changes/>
</rpc>
```

### 6.8. validate

Validates the contents of a configuration datastore.

```xml
<rpc message-id="110" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <validate>
    <source><candidate/></source>
  </validate>
</rpc>
```

TriadSim calls each model's `Validate() error` method to validate configuration. Invalid values result in an `` with `error-tag` = `invalid-value`.

### 6.9. close-session / kill-session

The `` operation gracefully terminates the NETCONF session. The `` operation forcefully terminates another session.

```xml
<rpc message-id="111" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <close-session/>
</rpc>
```

---

## 7. Event Notifications

### 7.1. Overview

NETCONF Event Notifications, defined in RFC 5277, provide an asynchronous message notification delivery service for NETCONF. This is an optional capability built on top of the base NETCONF definition .

TriadSim implements the notification capability with a single event stream named `sim-events`.

### 7.2. Subscribing

The client subscribes using the `` operation:

```xml
<rpc message-id="112" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <create-subscription xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">
    <stream>sim-events</stream>
  </create-subscription>
</rpc>
```

The server responds with ``. After subscription, the server sends `` elements asynchronously:

```xml
<notification xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">
  <eventTime>2024-01-15T10:30:00Z</eventTime>
  <event xmlns="urn:sim:events">
    <radio-link-down>
      <link>radio0</link>
      <severity>major</severity>
      <rssi>-82.5</rssi>
      <fade-margin>2.1</fade-margin>
    </radio-link-down>
  </event>
</notification>
```

### 7.3. Event Stream

The `sim-events` stream contains all event notifications supported by TriadSim:

| Event | Description |
|---|---|
| `radio-link-down` | Radio link failure or fade margin below threshold |
| `radio-link-up` | Radio link restored |
| `sync-holdover` | PTP transitioned to holdover state |
| `sync-restored` | PTP source restored, transitioned to master |
| `l2-storm-detected` | Broadcast storm threshold exceeded |
| `config-changed` | Running configuration modified |

### 7.4. Terminating Subscription

Subscriptions are terminated by closing the session or by using the `` operation. There is no explicit unsubscribe operation in RFC 5277; the subscription ends when the session ends .

---

## 8. Error Handling

### 8.1. RPC Error Format

When an operation fails, the server returns an `` element containing an ``:

```xml
<rpc-reply message-id="102" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <rpc-error>
    <error-type>protocol</error-type>
    <error-tag>invalid-value</error-tag>
    <error-severity>error</error-tag>
    <error-message>tx-power out of range: 999</error-message>
  </rpc-error>
</rpc-reply>
```

### 8.2. Error Tags

TriadSim uses the following error tags (from RFC 6241 Section 7.5.3):

| Error Tag | Description |
|---|---|
| `invalid-value` | Value fails validation |
| `missing-element` | Required element missing |
| `unknown-element` | Unrecognized element |
| `data-exists` | Create failed, data already exists |
| `data-missing` | Delete/remove failed, data missing |
| `operation-failed` | Operation failed for another reason |
| `lock-denied` | Datastore locked by another session |
| `in-use` | Resource in use |

### 8.3. Rollback on Error

When `` with `rollback-on-error` is used, if an error condition occurs such that an error severity `` element is generated, the server stops processing the `` operation and restores the specified configuration to its complete state at the start of this `` operation .

---

## 9. User-Flow Examples

### 9.1. Connecting

```bash
ssh -p 830 -s netconf admin@localhost
```

The server responds with ``:

```xml
<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <capabilities>
    <capability>urn:ietf:params:netconf:base:1.1</capability>
    <capability>urn:ietf:params:netconf:capability:candidate:1.0</capability>
    <capability>urn:ietf:params:netconf:capability:confirmed-commit:1.1</capability>
    <capability>urn:ietf:params:netconf:capability:notification:1.0</capability>
  </capabilities>
  <session-id>42</session-id>
</hello>
```

### 9.2. Configure Radio Link

```xml
<rpc message-id="1" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <edit-config>
    <target><candidate/></target>
    <config>
      <radio-link xmlns="urn:sim:radio-link">
        <name>radio0</name>
        <tx-power>20.0</tx-power>
        <atpc><enabled>true</enabled><target-rssi>-45.0</target-rssi></atpc>
        <acm><enabled>true</enabled></acm>
      </radio-link>
    </config>
  </edit-config>
</rpc>

<rpc message-id="2" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <commit/>
</rpc>

<rpc message-id="3" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <get-config>
    <source><running/></source>
    <filter type="subtree">
      <radio-link xmlns="urn:sim:radio-link"><name>radio0</name></radio-link>
    </filter>
  </get-config>
</rpc>
```

### 9.3. Discard Invalid Configuration

```xml
<rpc message-id="4" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <edit-config>
    <target><candidate/></target>
    <config>
      <radio-link xmlns="urn:sim:radio-link">
        <name>radio0</name>
        <tx-power>999</tx-power>
      </radio-link>
    </config>
  </edit-config>
</rpc>

<rpc-reply message-id="4" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <rpc-error>
    <error-type>protocol</error-type>
    <error-tag>invalid-value</error-tag>
    <error-severity>error</error-severity>
    <error-message>tx-power out of range: 999</error-message>
  </rpc-error>
</rpc-reply>

<rpc message-id="5" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <discard-changes/>
</rpc>
```

### 9.4. Subscribe to Notifications

```xml
<rpc message-id="6" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <create-subscription xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">
    <stream>sim-events</stream>
  </create-subscription>
</rpc>
```

After subscription, the client receives notifications as events occur:

```xml
<notification xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">
  <eventTime>2024-01-15T10:30:00Z</eventTime>
  <event xmlns="urn:sim:events">
    <radio-link-down>
      <link>radio0</link>
      <severity>major</severity>
    </radio-link-down>
  </event>
</notification>
```

---

## 10. References

| Document | Title |
|---|---|
| **RFC 6241** | Network Configuration Protocol (NETCONF)  |
| **RFC 6242** | Using the NETCONF Protocol over Secure Shell (SSH)  |
| **RFC 5277** | NETCONF Event Notifications  |
| **RFC 4741** | NETCONF Configuration Protocol (obsoleted by RFC 6241) |
| **RFC 4742** | Using the NETCONF Protocol over SSH (obsoleted by RFC 6242) |
| **RFC 6020** | YANG — A Data Modeling Language for NETCONF |
| **RFC 7950** | The YANG 1.1 Data Modeling Language |
| **RFC 8342** | Network Management Datastore Architecture (NMDA) |

---

## Appendix A: Capability URNs

| Capability | URN |
|---|---|
| Base 1.1 | `urn:ietf:params:netconf:base:1.1` |
| Candidate | `urn:ietf:params:netconf:capability:candidate:1.0` |
| Confirmed Commit 1.1 | `urn:ietf:params:netconf:capability:confirmed-commit:1.1` |
| Rollback on Error | `urn:ietf:params:netconf:capability:rollback-on-error:1.0` |
| Validate 1.1 | `urn:ietf:params:netconf:capability:validate:1.1` |
| Notification | `urn:ietf:params:netconf:capability:notification:1.0` |
| Writable Running | `urn:ietf:params:netconf:capability:writable-running:1.0` |

---

## Appendix B: NETCONF Subsystem Startup

The NETCONF subsystem is started when the client requests the `netconf` SSH subsystem. The server:

1. Accepts the SSH connection on port 830.
2. Processes the `subsystem` request with name `netconf`.
3. Sends `` with capabilities and session ID.
4. Receives client ``.
5. Enters the RPC loop, processing `` elements until session close.

The SSH server must default to allowing access to the `netconf` SSH subsystem only when using the specific TCP port assigned by IANA (830) .

---

## Appendix C: TriadSim YANG Modules

TriadSim does not parse YANG files at runtime. Instead, it uses Go structs with `path` tags for navigation. However, the following YANG modules are provided as documentation for clients:

| Module | Namespace | Purpose |
|---|---|---|
| `sim-device` | `urn:sim:device` | Device identity, system info |
| `sim-radio-link` | `urn:sim:radio-link` | Radio link configuration and state |
| `sim-l2-switching` | `urn:sim:l2-switching` | VLAN, MAC table, STP, LLDP |
| `sim-sync` | `urn:sim:sync` | PTP, SyncE, holdover |

These YANG modules are embedded in the binary via `//go:embed` and served through the CLI `simulator schema --yang` command. They are not used for runtime validation.
