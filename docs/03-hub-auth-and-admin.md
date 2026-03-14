# MaClaw Hub Auth And Admin

## 1. Goal
This document defines how MaClaw Hub handles:

- user identity
- SN issuance
- machine credentials
- email confirmation login
- admin backend initialization
- governance actions

## 2. Identity Principles

### 2.1 Hub-local identity
Each Hub manages its own users. There is no global auth server.

### 2.2 SN remains important
SN is still a hub-issued identity value, but it is no longer the primary mobile login input.

SN is mainly used for:

- Desktop enrollment
- hub-internal user identity
- admin views and manual binding

### 2.3 PWA login uses email confirmation
PWA users should:

- enter email
- receive a sign-in email
- click the confirmation link
- receive a viewer token

This avoids making users type SN on phones.

## 3. Hub Modes

### `open`
- email is accepted immediately
- Hub may create the user automatically
- Hub issues SN

### `approval`
- email waits for admin approval
- SN is issued after approval

### `manual`
- only admin-bound emails are allowed
- SN is issued during manual binding

## 4. Desktop Enrollment

Desktop uses:

- `POST /api/enroll/start`

The Hub returns:

- `email`
- `sn`
- `user_id`
- `machine_id`
- `machine_token`

Desktop then uses `machine_token` for future Hub communication.

## 5. PWA Login

PWA uses two-step login:

### Step 1
- `POST /api/auth/email-request`

### Step 2
- `POST /api/auth/email-confirm`

On success, Hub returns a viewer token.

## 6. Admin Backend

Both Hub and Hub Center require first-time admin setup:

- admin username
- admin password
- admin email

Hub admin backend should later support:

- enrollment mode management
- manual bind
- email blocklist
- invite flow
- approval flow
- mail settings
- audit views

## 7. Governance

Hub governance should follow:

- blocklist first
- explicit allow second
- visibility defaults last

This means deny rules override any open/shared defaults.

## 8. Mail Capability

Hub should support outbound email for:

- mobile sign-in confirmation
- invite messages
- approval notifications
- admin notifications

## 9. Current Phase Boundary

Currently required:

- admin setup
- admin login skeleton
- desktop enrollment
- email confirmation login skeleton
- machine authentication

Later:

- approval workflow
- invite workflow
- richer admin UI
