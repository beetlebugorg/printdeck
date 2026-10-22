// The connection of the window to the deck: the process of printdeck app, the stream of the deck
// UI (/ui/events) and the touches (/ui/touch). DeckCanvas.qml draws the frames.
#pragma once

#include <QByteArray>
#include <QNetworkAccessManager>
#include <QObject>
#include <QPointer>
#include <QProcess>
#include <QQmlNetworkAccessManagerFactory>
#include <QSize>
#include <QTimer>
#include <QUrl>

class QNetworkReply;

class Deck : public QObject
{
    Q_OBJECT
    // "starting" (printdeck app starts), "connecting", "live" (a frame shows), or "failed".
    Q_PROPERTY(QString view READ view NOTIFY screenChanged)
    Q_PROPERTY(QString error READ error NOTIFY screenChanged)
    Q_PROPERTY(QString host READ host CONSTANT)
    Q_PROPERTY(int screenWidth READ screenWidth NOTIFY screenChanged)
    Q_PROPERTY(int screenHeight READ screenHeight NOTIFY screenChanged)
    Q_PROPERTY(QString frame READ frame NOTIFY frameChanged)
    Q_PROPERTY(QString imageBase READ imageBase NOTIFY screenChanged)
    Q_PROPERTY(QString fontBase READ fontBase NOTIFY screenChanged)
    Q_PROPERTY(bool fontsReady READ fontsReady NOTIFY screenChanged)

public:
    explicit Deck(QObject *parent = nullptr);
    ~Deck() override;

    // start starts printdeck app. program is the path of the printdeck program.
    void start(const QString &program, const QStringList &args);
    // attach connects to a printdeck app that runs already: hello is its first line of output.
    bool attach(const QByteArray &hello);
    // stop ends printdeck app: it ends when its input closes.
    void stop();

    QString view() const;
    QString error() const { return m_error; }
    QString host() const { return QStringLiteral("this computer"); }
    int screenWidth() const { return m_screen.width(); }
    int screenHeight() const { return m_screen.height(); }
    QString frame() const { return m_frame; }
    QString imageBase() const;
    QString fontBase() const;
    bool fontsReady() const { return m_base.isValid(); }

    // The address of the web server and the token, for the requests of QML (Factory).
    QUrl base() const { return m_base; }
    QByteArray token() const { return m_token; }

    // setArea gives the size of the window for the deck UI. A new shape asks for a new session.
    Q_INVOKABLE void setArea(qreal w, qreal h);
    // touch sends a touch: kind is "down", "move" or "up", and x and y are 0..1 of the screen.
    Q_INVOKABLE void touch(const QString &kind, qreal x, qreal y);
    // key sends a key of the keyboard of the computer: the text that it types, or the name of a
    // key with no text ("Enter", "Escape" or "Backspace").
    Q_INVOKABLE void key(const QString &text, const QString &name);

signals:
    void screenChanged();
    void frameChanged();

private:
    void readHello();
    void connectStream();
    void readStream();
    void streamEnded();
    void event(const QByteArray &name, const QByteArray &data);
    void fail(const QString &error);
    void sendKey();

    QProcess m_process;
    QByteArray m_out; // the output of printdeck app before the first line
    QUrl m_base;
    QByteArray m_token;

    QNetworkAccessManager m_net;
    QPointer<QNetworkReply> m_stream;
    QByteArray m_buffer;           // the part of the stream that is not a full line yet
    QByteArray m_name, m_data;     // the event that is read now
    QString m_session;
    bool m_live = false;
    QString m_error;
    QString m_frame;
    QSize m_screen{800, 480};
    QSizeF m_area;
    QTimer m_retry;
    QList<QByteArray> m_keys; // the keys to send, in order
    bool m_keySending = false;
};

// Factory gives QML a network manager that sends the token in the requests to the deck: the
// images of the frames, and the fonts.
class Factory : public QQmlNetworkAccessManagerFactory
{
public:
    explicit Factory(Deck *deck) : m_deck(deck) {}
    QNetworkAccessManager *create(QObject *parent) override;

private:
    Deck *m_deck;
};
