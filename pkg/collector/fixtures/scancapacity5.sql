INSERT INTO cluster_capacitors (cluster_id, capacitor_id, scraped_at, scrape_duration_secs, serialized_metrics) VALUES ('west', 'unittest', 10, 1, '');
INSERT INTO cluster_capacitors (cluster_id, capacitor_id, scraped_at, scrape_duration_secs, serialized_metrics) VALUES ('west', 'unittest2', 10, 1, '');
INSERT INTO cluster_capacitors (cluster_id, capacitor_id, scraped_at, scrape_duration_secs, serialized_metrics) VALUES ('west', 'unittest4', 10, 1, '{"smaller_half":14,"larger_half":28}');

INSERT INTO cluster_resources (service_id, name, capacity, subcapacities, capacity_per_az) VALUES (1, 'things', 23, '', '');
INSERT INTO cluster_resources (service_id, name, capacity, subcapacities, capacity_per_az) VALUES (2, 'capacity', 42, '', '');
INSERT INTO cluster_resources (service_id, name, capacity, subcapacities, capacity_per_az) VALUES (2, 'things', 42, '[{"smaller_half":14},{"larger_half":28}]', '');

INSERT INTO cluster_services (id, cluster_id, type, scraped_at) VALUES (1, 'shared', 'shared', 10);
INSERT INTO cluster_services (id, cluster_id, type, scraped_at) VALUES (2, 'west', 'unshared', 10);
