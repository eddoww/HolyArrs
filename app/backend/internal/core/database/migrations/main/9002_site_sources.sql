-- Fork builds before 1.1.4 rebuilt sources here to add a format per site
-- (iptorrents, knaben, subsplease, torrentscsv) and the columns user_agent
-- and freeleech_only. Upstream 1.1.4 has its own built-in sites (0121:
-- format 'site', the site in extra), so this step changes nothing now; on a
-- new database it runs after 0121 and must not narrow its format check
-- again. A database that ran the old step has its site rows carried into
-- upstream's form by the runner's fork hook around 0121 (fork_hooks.go). The
-- file stays, under its name, because the runner refuses a recorded
-- migration it does not know.
SELECT 1;
