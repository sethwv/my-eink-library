---
title: Tasks
parent: User Guide
nav_order: 7
---

# Tasks

Open **Tasks** from the Admin tabs to monitor queued maintenance work. The page refreshes every ten seconds while open. Select **Pause refresh** when reading a changing table or troubleshooting a task.

## Run a task

**Scan Library** checks configured library folders and updates the derived index. It runs during server startup and can also be queued on demand. Only one scan can be queued or running at a time. The checkmark replaces the play control while the task is active.

**Email digest** sends the weekly new-book digest to subscribed users. SMTP must be configured and enabled before it can run; otherwise its play control is unavailable and manual requests are rejected. Its next scheduled send is shown relative to the current time, and the play control sends a digest immediately.

Tasks run one at a time. The History table shows the ten most recent task runs with their status, queued time, start time, and duration.

{% include screenshot-pair.html id="admin-tasks-guide" %}

## Rebuild the index

Use **Clear library and queue scan** on the Server tab only when the derived index needs a complete rebuild. It clears indexed books, enrichment state, locations, and shelf memberships, then queues **Scan Library**. It does not remove the shelf definitions themselves.
