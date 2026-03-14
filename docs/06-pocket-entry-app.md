# MaClaw Pocket Entry App

## 1. Goal
MaClaw Pocket is the lightweight Android/iOS entry app for the remote-control system.

It should help users:

- enter their email
- resolve the correct hub entry
- open the correct PWA

## 2. Scope

Pocket is not:

- a full IDE
- the primary remote control UI
- a session host

Pocket is:

- a launcher
- a hub entry client
- a future notification container

## 3. Flow

1. User opens Pocket.
2. User enters email.
3. Pocket calls Hub Center `entry/resolve`.
4. Pocket opens the returned PWA URL.
5. PWA handles email-confirm login with the Hub.

## 4. Minimal Pages

### Splash
- last email
- last hub shortcut

### Email Entry
- email field
- continue button

### Hub Picker
- shown only when multiple hubs are returned

## 5. Platform Strategy

Recommended first implementation:

- Android: Custom Tabs or default browser
- iOS: Safari or SFSafariViewController

## 6. Future Expansion

Later Pocket may add:

- notification entry points
- direct session deep links
- favorite hubs
- shared-hub entry shortcuts
