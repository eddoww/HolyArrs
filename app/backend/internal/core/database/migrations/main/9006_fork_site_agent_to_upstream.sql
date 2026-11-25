-- A database a fork build updated keeps an IPTorrents Source's browser user
-- agent under the fork's extra key user_agent; upstream 1.3.0 signs in with
-- a cookie of its own (site_login 'cookie', user agent in site_agent). The
-- fork only ever signed in to IPTorrents with a cookie, so its Sources move
-- to upstream's cookie sign-in. A Source with upstream's user id is left
-- alone. On a new database no row has the fork's key, so nothing changes.
UPDATE sources SET extra = json_set(json_remove(extra, '$.user_agent'),
    '$.site_login', 'cookie',
    '$.site_agent', coalesce(nullif(json_extract(extra, '$.site_agent'), ''), json_extract(extra, '$.user_agent'), ''))
  WHERE format = 'site' AND json_valid(extra) AND json_type(extra) = 'object'
    AND json_extract(extra, '$.site') = 'iptorrents'
    AND json_type(extra, '$.user_agent') IS NOT NULL
    AND coalesce(json_extract(extra, '$.site_user'), '') = '';

-- The fork's user agent on any other Source (no site signs in but
-- IPTorrents) is dropped.
UPDATE sources SET extra = json_remove(extra, '$.user_agent')
  WHERE json_valid(extra) AND json_type(extra) = 'object' AND json_type(extra, '$.user_agent') IS NOT NULL;
