-- Before upstream's 0121 on a database a fork build updated: the fork's site
-- Sources take upstream's extra and limits, so 0121 keeps every value. The
-- site, user agent and freeleech choice go into extra as strings (a
-- Source's extra is a string map); each missing limit takes the site's
-- polite default, with the fork's 2 second floor between calls. 0121's own
-- copy then reads their format as 'site' (fork_hooks.go).
UPDATE sources SET
  extra = json_set(CASE WHEN json_valid(extra) AND json_type(extra) = 'object' THEN extra ELSE '{}' END,
    '$.site', format, '$.user_agent', user_agent, '$.freeleech_only', CASE freeleech_only WHEN 1 THEN '1' ELSE '' END),
  limits = json_set(CASE WHEN json_valid(limits) AND json_type(limits) = 'object' THEN limits ELSE '{}' END,
    '$.gap_ms', max(coalesce(json_extract(limits, '$.gap_ms'), 0), CASE format WHEN 'iptorrents' THEN 3000 ELSE 2000 END),
    '$.searches_per_window', coalesce(json_extract(limits, '$.searches_per_window'), CASE format WHEN 'iptorrents' THEN 60 ELSE 100 END),
    '$.window_seconds', coalesce(json_extract(limits, '$.window_seconds'), 3600),
    '$.timeout_ms', coalesce(json_extract(limits, '$.timeout_ms'), CASE format WHEN 'iptorrents' THEN 30000 ELSE 20000 END))
  WHERE format IN ('iptorrents', 'knaben', 'subsplease', 'torrentscsv');
