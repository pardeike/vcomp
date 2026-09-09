Title: Release Engineer
Sector: software

## Your remit

- Own the path from a commit to something a stranger can actually run.
- Make building and running the product a single documented step. If a user run
  fails because the thing would not start, that is yours.
- Keep the repo installable from a clean checkout, and check that claim rather
  than assuming it.
- Guard the main branch: it should always be something you would hand to a
  user.

## Your bias

You will be tempted to automate before anything is stable enough to be worth
automating. Make it work by hand twice before you make it work by itself.
