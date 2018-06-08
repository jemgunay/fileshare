$(document).ready(function() {
    // animate burger menu button
    $('#navbar').on('hide.bs.collapse show.bs.collapse', function () {
        $('#nav-animated-icon').toggleClass('open');
    });
});

// A logger which logs to console only if debug is enabled & maintains a history of all logs.
function Logger(outputDebug) {
    // A record of all debug logs
    var history = ["Logs since: " + new Date().toLocaleString()];

    // Perform a standard console write.
    this.log = function (msg) {
        console.log(msg);
        history.push({msg: msg, stack: getStackTrace()})
    };

    // Perform a debug console write.
    this.debugLog = function (msg) {
        msg = "[debug] " + msg;
        if (outputDebug === true) {
            console.log(msg);
        }
        history.push({msg: msg, stack: getStackTrace()})
    };

    // Enable debug logging.
    this.disableDebug = function () {
        outputDebug = true;
    };

    // Disable debug logging.
    this.disableDebug = function () {
        outputDebug = false;
    };

    this.dumpLogs = function () {
        console.log(history);
    };
}

function getStackTrace () {
    var stack;

    try {
        throw new Error('');
    }
    catch (error) {
        stack = error.stack || '';
    }

    stack = stack.split('\n').map(function (line) { return line.trim(); });
    return stack.splice(stack[0] == 'Error' ? 2 : 1);
}

// Perform basic AJAX request.
function performRequest(URL, httpMethod, data, resultMethod) {
    logger.debugLog("> [" + httpMethod + "] " + URL + ": " + data);
    $.ajax({
        url: URL,
        type: httpMethod,
        dataType: 'text',
        data: data,
        error: function(e) {
            logger.debugLog(e);
            notifier.queueAlert("Could not connect to the server.", "danger");
        },
        success: function(e) {
            logger.debugLog(e);
            resultMethod(e);
        },
        timeout: 10000
    });
}

// HTML for window alert.
var alertWindowHTML = "";

// Set an alert window warning/error HTML message and display.
function setAlertWindow(type, msg, target) {
    // set window
    var setAlert = function() {
        var replaced = alertWindowHTML.replace("{{type}}", type);
        replaced = replaced.replace("{{msg}}", msg);
        if (typeof target === 'string' || target instanceof String) {
            target = $(target);
        }
        target.hide().empty().append(replaced).fadeIn(400);
    };

    // fetch window html via GET request
    if (alertWindowHTML === "") {
        performRequest(hostname + "/static/templates/alert.html", "GET", "", function (result) {
            alertWindowHTML = result;
            setAlert();
        });
    } else {
        // use already acquired HTML
        setAlert();
    }
}

// A queue for notify alerts to limit number of alerts on screen at a time.
function AlertNotifier(maxAlerts, delay) {
    var notifyQueue = [];
    var alertCount = 0;

    // Show alert if no alerts waiting in queue, else add alert to queue.
    this.queueAlert = function(msg, type) {
        var alert = {msg: msg, type: type};

        if (alertCount < maxAlerts) {
            this.showAlert(alert);
            return;
        }

        notifyQueue.push(alert);
    };

    // Show an alert.
    this.showAlert = function(alert) {
        alertCount++;

        var icon = "glyphicon glyphicon-ok";
        if (alert.type === "warning" || alert.type === "danger") {
            icon = "glyphicon glyphicon-remove"
        }

        var self = this;

        $.notify({
            message: "<strong>" + alert.msg + "</strong>",
            icon: icon
        }, {
            type: alert.type,
            delay: delay,
            newest_on_top: true,
            mouse_over: "pause",
            onClosed: function() {
                self.onAlertClose();
            }
        });
    };

    // Triggered when an alert closes; checks the head of the queue for an alert to show.
    this.onAlertClose = function() {
        if (notifyQueue.length > 0) {
            var poppedAlert = notifyQueue.shift();
            this.showAlert(poppedAlert)
        }
        alertCount--;
    };
}

// Toggle button enabled & spinner visibility.
function setButtonProcessing(element, enabled) {
    if (enabled === true) {
        element.find(".btn-label").css("display", "none");
        element.find(".btn-spinner").css("display", "inline-block");
        element.attr("disabled", true);
        return
    }
    element.find(".btn-label").css("display", "inline-block");
    element.find(".btn-spinner").css("display", "none");
    element.attr("disabled", false);
}

// Parse form data into a JSON object.
function formToJSON(form) {
    var arr = form.serializeArray();
    var returnArray = {};
    for (var i = 0; i < arr.length; i++){
        returnArray[arr[i]['name']] = arr[i]['value'];
    }
    return JSON.stringify(returnArray);
}

/* Global vars */
var hostname = location.protocol + '//' + location.host;
// Create an instance of an AlertNotifier.
var notifier = new AlertNotifier(5, 4000);
// Create an instance of a Logger.
var logger = new Logger(false);