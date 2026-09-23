# Vendored schemas

Pinned copies of the [Agent Plugins](https://agent-plugins.org) 1.0.0 JSON
Schemas, fetched from `https://agent-plugins.org/schemas/1.0.0/`.

They are here so `make plugin-check` validates offline and deterministically. A
check that fetched them would fail when someone else's site is down, which is the
same reasoning `scripts/check-doc-links.sh` already applies to external URLs —
and a validation failure that means "the network was unavailable" trains people
to ignore validation failures.

The live copies are fetched and diffed against these by `make plugin-upstream-check`
in the weekly sweep, which is where an upstream revision belongs: it is a signal
about somebody else's schedule rather than about the pull request in front of
you. The same job notices a newer spec version being published beside 1.0.0,
which the diff alone cannot see.

Do not edit them. If a diff shows upstream changed, take the new copy and fix
whatever it now rejects.
