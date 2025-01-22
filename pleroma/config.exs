import Config

# 基础配置
config :pleroma, :configurable_from_database, true

# 中继配置
config :pleroma, :relays, enabled: true
  
# 终端点配置
config :pleroma, Pleroma.Web.Endpoint,
  url: [host: System.fetch_env!("DOMAIN"), scheme: "https", port: 443],
  http: [ip: {0, 0, 0, 0}, port: 4000]

# 实例配置
config :pleroma, :instance,
  name: System.fetch_env!("INSTANCE_NAME"),
  email: System.fetch_env!("ADMIN_EMAIL"),
  notify_email: System.fetch_env!("NOTIFY_EMAIL"),
  limit: 5000,
  registrations_open: true,
  federating: true,
  healthcheck: true,
  static_dir: "/var/lib/pleroma/static"

# 媒体代理配置
config :pleroma, :media_proxy,
  enabled: false,
  redirect_on_failure: false

# 数据库配置
config :pleroma, Pleroma.Repo,
  adapter: Ecto.Adapters.Postgres,
  username: System.fetch_env!("DB_USER"),
  password: System.fetch_env!("DB_PASS"),
  database: System.fetch_env!("DB_NAME"),
  hostname: System.fetch_env!("DB_HOST"),
  pool_size: 10

# Web Push 通知配置
config :web_push_encryption, :vapid_details,
  subject: "mailto:#{System.fetch_env!("NOTIFY_EMAIL")}"

# 数据库扩展配置
config :pleroma, :database, rum_enabled: false

# 上传配置
config :pleroma, Pleroma.Uploaders.Local, uploads: "/var/lib/pleroma/uploads"

# 密钥配置
if not File.exists?("/var/lib/pleroma/secret.exs") do
  secret = :crypto.strong_rand_bytes(64) |> Base.encode64() |> binary_part(0, 64)
  signing_salt = :crypto.strong_rand_bytes(8) |> Base.encode64() |> binary_part(0, 8)
  {web_push_public_key, web_push_private_key} = :crypto.generate_key(:ecdh, :prime256v1)

  secret_file =
    EEx.eval_string(
      """
      import Config

      config :pleroma, Pleroma.Web.Endpoint,
        secret_key_base: "<%= secret %>",
        signing_salt: "<%= signing_salt %>"

      config :web_push_encryption, :vapid_details,
        public_key: "<%= web_push_public_key %>",
        private_key: "<%= web_push_private_key %>"
      """,
      secret: secret,
      signing_salt: signing_salt,
      web_push_public_key: Base.url_encode64(web_push_public_key, padding: false),
      web_push_private_key: Base.url_encode64(web_push_private_key, padding: false)
    )

  File.write("/var/lib/pleroma/secret.exs", secret_file)
end

import_config("/var/lib/pleroma/secret.exs")

# 额外的用户配置
if File.exists?("/var/lib/pleroma/config.exs") do
  import_config("/var/lib/pleroma/config.exs")
else
  File.write("/var/lib/pleroma/config.exs", """
  import Config

  # 在这里添加额外的配置
  """)
end
