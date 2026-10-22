#include "deck.h"

#include <QJsonDocument>
#include <QJsonObject>
#include <QNetworkReply>
#include <QNetworkRequest>
#include <QUrlQuery>
#include <cmath>

namespace {

// The size of the deck UI, as wanted_size of the Cura plugin: the wide screen for a window that is
// wider than high, else the tall screen with the shape of the window.
const QSize wide(800, 480);
constexpr int tallW = 480, minH = 800, maxH = 1200;

QSize wantedSize(qreal w, qreal h)
{
    if (w <= 0 || h <= 0 || w > h)
        return wide;
    return QSize(tallW, std::clamp(int(std::lround(tallW * h / w)), minH, maxH));
}

// The wait before a new connection after the stream ended, in milliseconds.
constexpr int retryMs = 1000;

} // namespace

Deck::Deck(QObject *parent) : QObject(parent)
{
    m_retry.setSingleShot(true);
    connect(&m_retry, &QTimer::timeout, this, &Deck::connectStream);
    // The log of printdeck app goes to the log of the window.
    m_process.setProcessChannelMode(QProcess::ForwardedErrorChannel);
    connect(&m_process, &QProcess::readyReadStandardOutput, this, &Deck::readHello);
    connect(&m_process, &QProcess::errorOccurred, this, [this](QProcess::ProcessError) {
        fail(QStringLiteral("printdeck app: %1").arg(m_process.errorString()));
    });
    connect(&m_process, &QProcess::finished, this, [this](int code, QProcess::ExitStatus) {
        fail(QStringLiteral("printdeck app ended (exit code %1)").arg(code));
    });
}

Deck::~Deck()
{
    stop();
}

void Deck::start(const QString &program, const QStringList &args)
{
    m_process.start(program, args);
}

void Deck::stop()
{
    m_retry.stop();
    if (m_stream)
        m_stream->abort();
    if (m_process.state() == QProcess::NotRunning)
        return;
    m_process.disconnect(this);
    m_process.closeWriteChannel();
    if (!m_process.waitForFinished(5000))
        m_process.kill();
}

QString Deck::view() const
{
    if (!m_error.isEmpty())
        return QStringLiteral("failed");
    if (m_live)
        return QStringLiteral("live");
    return m_base.isValid() ? QStringLiteral("connecting") : QStringLiteral("starting");
}

QString Deck::imageBase() const
{
    return m_base.isValid() ? m_base.toString() + QStringLiteral("/ui/img/") : QString();
}

QString Deck::fontBase() const
{
    return m_base.isValid() ? m_base.toString() + QStringLiteral("/ui/font/") : QString();
}

// readHello reads the first line of printdeck app: {"url": ..., "token": ...}.
void Deck::readHello()
{
    m_out += m_process.readAllStandardOutput();
    if (m_base.isValid())
        return;
    int nl = m_out.indexOf('\n');
    if (nl < 0)
        return;
    QJsonObject hello = QJsonDocument::fromJson(m_out.left(nl)).object();
    m_out.clear();
    QUrl url(hello.value(QStringLiteral("url")).toString());
    QByteArray token = hello.value(QStringLiteral("token")).toString().toUtf8();
    if (!url.isValid() || token.isEmpty()) {
        fail(QStringLiteral("printdeck app gave no address"));
        return;
    }
    m_base = url;
    m_token = token;
    emit screenChanged();
    connectStream();
}

void Deck::connectStream()
{
    if (!m_base.isValid() || m_stream)
        return;
    m_screen = wantedSize(m_area.width(), m_area.height());
    QUrl url = m_base.resolved(QUrl(QStringLiteral("/ui/events")));
    QUrlQuery q;
    q.addQueryItem(QStringLiteral("w"), QString::number(m_screen.width()));
    q.addQueryItem(QStringLiteral("h"), QString::number(m_screen.height()));
    url.setQuery(q);
    QNetworkRequest req(url);
    req.setRawHeader("X-Api-Key", m_token);
    req.setRawHeader("Accept", "text/event-stream");
    req.setAttribute(QNetworkRequest::CacheLoadControlAttribute, QNetworkRequest::AlwaysNetwork);
    req.setTransferTimeout(75000); // the deck sends a ping each 30 seconds
    m_buffer.clear();
    m_name.clear();
    m_data.clear();
    m_stream = m_net.get(req);
    connect(m_stream, &QNetworkReply::readyRead, this, &Deck::readStream);
    connect(m_stream, &QNetworkReply::finished, this, &Deck::streamEnded);
}

// readStream reads the lines of the Server-Sent Events.
void Deck::readStream()
{
    m_buffer += m_stream->readAll();
    int nl;
    while ((nl = m_buffer.indexOf('\n')) >= 0) {
        QByteArray line = m_buffer.left(nl);
        m_buffer.remove(0, nl + 1);
        if (line.endsWith('\r'))
            line.chop(1);
        if (line.isEmpty()) {
            if (!m_data.isEmpty())
                event(m_name.isEmpty() ? QByteArray("message") : m_name, m_data);
            m_name.clear();
            m_data.clear();
        } else if (line.startsWith("event:")) {
            m_name = line.mid(6).trimmed();
        } else if (line.startsWith("data:")) {
            if (!m_data.isEmpty())
                m_data += '\n';
            m_data += line.mid(5).trimmed();
        }
    }
}

void Deck::event(const QByteArray &name, const QByteArray &data)
{
    if (name == "session") {
        // "ID W H": the number of the session, for the touches, and the size of its screen.
        QList<QByteArray> f = data.split(' ');
        if (f.size() == 3) {
            m_session = QString::fromUtf8(f[0]);
            m_screen = QSize(f[1].toInt(), f[2].toInt());
            emit screenChanged();
        }
        return;
    }
    m_frame = QString::fromUtf8(data);
    if (!m_live) {
        m_live = true;
        emit screenChanged();
    }
    emit frameChanged();
}

void Deck::streamEnded()
{
    QNetworkReply *r = m_stream;
    m_stream = nullptr;
    if (r) {
        int status = r->attribute(QNetworkRequest::HttpStatusCodeAttribute).toInt();
        if (status == 401)
            qWarning("printdeck: the deck refused the token");
        r->deleteLater();
    }
    m_session.clear();
    if (m_live) {
        m_live = false;
        emit screenChanged();
    }
    if (m_error.isEmpty())
        m_retry.start(retryMs);
}

void Deck::setArea(qreal w, qreal h)
{
    m_area = QSizeF(w, h);
    QSize want = wantedSize(w, h);
    if (m_stream && (want.width() != m_screen.width() || std::abs(want.height() - m_screen.height()) > 8))
        m_stream->abort(); // streamEnded connects again, with the new size
}

void Deck::touch(const QString &kind, qreal x, qreal y)
{
    if (m_session.isEmpty())
        return;
    QUrl url = m_base.resolved(QUrl(QStringLiteral("/ui/touch")));
    url.setQuery(QStringLiteral("s=") + m_session);
    QNetworkRequest req(url);
    req.setRawHeader("X-Api-Key", m_token);
    req.setHeader(QNetworkRequest::ContentTypeHeader, QStringLiteral("application/json"));
    QJsonObject body{{QStringLiteral("kind"), kind},
                     {QStringLiteral("x"), std::clamp(x, 0.0, 1.0)},
                     {QStringLiteral("y"), std::clamp(y, 0.0, 1.0)}};
    QNetworkReply *r = m_net.post(req, QJsonDocument(body).toJson(QJsonDocument::Compact));
    connect(r, &QNetworkReply::finished, r, &QObject::deleteLater);
}

void Deck::key(const QString &text, const QString &name)
{
    if (m_session.isEmpty())
        return;
    QJsonObject body;
    if (name.isEmpty())
        body.insert(QStringLiteral("text"), text);
    else
        body.insert(QStringLiteral("name"), name);
    m_keys.append(QJsonDocument(body).toJson(QJsonDocument::Compact));
    sendKey();
}

// sendKey sends the next key of the queue, one at a time, so fast typing keeps its order.
void Deck::sendKey()
{
    if (m_keySending || m_keys.isEmpty() || m_session.isEmpty())
        return;
    QUrl url = m_base.resolved(QUrl(QStringLiteral("/ui/key")));
    url.setQuery(QStringLiteral("s=") + m_session);
    QNetworkRequest req(url);
    req.setRawHeader("X-Api-Key", m_token);
    req.setHeader(QNetworkRequest::ContentTypeHeader, QStringLiteral("application/json"));
    m_keySending = true;
    QNetworkReply *r = m_net.post(req, m_keys.takeFirst());
    connect(r, &QNetworkReply::finished, this, [this, r] {
        r->deleteLater();
        m_keySending = false;
        sendKey();
    });
}

void Deck::fail(const QString &error)
{
    if (!m_error.isEmpty())
        return;
    qWarning("printdeck: %s", qPrintable(error));
    m_error = error;
    m_live = false;
    m_retry.stop();
    emit screenChanged();
}

namespace {

// TokenManager adds the token to the requests to the deck.
class TokenManager : public QNetworkAccessManager
{
public:
    TokenManager(Deck *deck, QObject *parent) : QNetworkAccessManager(parent), m_deck(deck) {}

protected:
    QNetworkReply *createRequest(Operation op, const QNetworkRequest &req, QIODevice *data) override
    {
        QUrl base = m_deck->base();
        if (base.isValid() && req.url().scheme() == base.scheme() && req.url().host() == base.host()
            && req.url().port() == base.port()) {
            QNetworkRequest r(req);
            r.setRawHeader("X-Api-Key", m_deck->token());
            return QNetworkAccessManager::createRequest(op, r, data);
        }
        return QNetworkAccessManager::createRequest(op, req, data);
    }

private:
    Deck *m_deck;
};

} // namespace

QNetworkAccessManager *Factory::create(QObject *parent)
{
    return new TokenManager(m_deck, parent);
}
